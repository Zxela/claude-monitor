#!/usr/bin/env python3
"""Validate a running claude-monitor against api/openapi.yaml.

Checks three things that ordinary tests miss:

  1. Response bodies against the declared schema (missing required fields,
     wrong types) — the spec promising something the server does not deliver.
  2. Response fields absent from the schema — the server returning something
     the spec does not document. Plain JSON-Schema validation cannot catch this
     because OpenAPI objects are open by default, so this walks the payload
     against the schema and reports unknown keys.
  3. Every route registered in cmd/claude-monitor/main.go appears in the spec,
     so a new handler cannot ship undocumented.

Usage:
    python3 api/validate_spec.py [BASE_URL] [SPEC_PATH] [MAIN_GO_PATH]

Defaults: http://127.0.0.1:7700, api/openapi.yaml, cmd/claude-monitor/main.go
Exits 0 when clean, 1 when any ERROR is found. DRIFT is reported and also
fails the run; SKIP is informational.

Endpoints taking a path id are exercised only when the database has data to
name. Against an empty database those checks report SKIP rather than passing
silently, so CI output states plainly what it did not cover.

Requires: pyyaml, jsonschema
"""

import json
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

import jsonschema
import yaml

BASE = (sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:7700").rstrip("/")
SPEC_PATH = sys.argv[2] if len(sys.argv) > 2 else "api/openapi.yaml"
MAIN_GO = sys.argv[3] if len(sys.argv) > 3 else "cmd/claude-monitor/main.go"

spec = yaml.safe_load(open(SPEC_PATH))
results = []  # (severity, endpoint, message)


def add(sev, ep, msg):
    results.append((sev, ep, msg))


def deref(node, seen=None):
    """Resolve $ref and translate OpenAPI 3.0 `nullable` into JSON Schema.

    jsonschema ignores unknown keywords, so a `nullable: true` field typed
    `string` would otherwise reject a legitimate null.
    """
    if seen is None:
        seen = set()
    if isinstance(node, dict):
        if "$ref" in node:
            ref = node["$ref"]
            if ref in seen:  # recursive schema — stop unrolling
                return {}
            target = spec
            for part in ref.lstrip("#/").split("/"):
                target = target[part]
            return deref(target, seen | {ref})
        out = {k: deref(v, seen) for k, v in node.items() if k != "nullable"}
        if node.get("nullable") and "type" in out:
            t = out["type"]
            out["type"] = [t, "null"] if isinstance(t, str) else list(t) + ["null"]
        return out
    if isinstance(node, list):
        return [deref(v, seen) for v in node]
    return node


def undocumented(instance, schema, path=""):
    """Report instance keys with no counterpart in the schema."""
    found = []
    if isinstance(instance, dict) and isinstance(schema, dict):
        props = schema.get("properties")
        extra = schema.get("additionalProperties")
        if props is not None and not extra:
            for key, val in instance.items():
                here = f"{path}.{key}" if path else key
                if key not in props:
                    found.append(here)
                else:
                    found += undocumented(val, props[key], here)
        elif isinstance(extra, dict):
            for key, val in instance.items():
                found += undocumented(val, extra, f"{path}.{key}" if path else key)
    elif isinstance(instance, list) and isinstance(schema, dict):
        items = schema.get("items")
        if items:
            for i, val in enumerate(instance[:5]):  # sample the first few
                found += undocumented(val, items, f"{path}[{i}]")
    return found


def request(method, url, body=None):
    req = urllib.request.Request(url, method=method)
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, data, timeout=120) as r:
            return r.status, r.read(), r.headers
    except urllib.error.HTTPError as e:
        return e.code, e.read(), e.headers
    except Exception as e:  # connection refused, timeout, ...
        return None, str(e).encode(), {}


def check(method, tmpl, url, expect="200", note="", body=None):
    """Exercise one operation and validate it against the spec."""
    ep = f"{method} {tmpl}{(' ' + note) if note else ''}"
    op = spec["paths"].get(tmpl, {}).get(method.lower())
    if op is None:
        add("ERROR", ep, "path/method not present in spec")
        return None

    status, raw, _ = request(method, url, body)
    if status is None:
        add("ERROR", ep, f"request failed: {raw.decode()[:200]}")
        return None
    if str(status) != expect:
        add(
            "ERROR",
            ep,
            f"expected HTTP {expect}, got {status}: "
            f"{raw[:200].decode(errors='replace')}",
        )

    resp = op.get("responses", {}).get(str(status))
    if resp is None:
        add("ERROR", ep, f"HTTP {status} returned but not documented in spec")
        return None
    content = resp.get("content", {}).get("application/json")
    if content is None:
        return None  # nothing to validate (e.g. 204)

    try:
        payload = json.loads(raw)
    except Exception:
        add("ERROR", ep, f"HTTP {status} body is not valid JSON: {raw[:120]}")
        return None

    schema = deref(content["schema"])
    try:
        jsonschema.validate(payload, schema)
    except jsonschema.ValidationError as e:
        loc = "/".join(str(p) for p in e.absolute_path) or "(root)"
        add("ERROR", ep, f"HTTP {status} schema violation at `{loc}`: {e.message[:300]}")
    for f in sorted(set(undocumented(payload, schema))):
        add("DRIFT", ep, f"response field not in spec: `{f}`")
    return payload


def window_enum(tmpl):
    """The window values this operation's own spec declares.

    Returns [] when the path or its get operation is missing, so a route absent
    from the spec is reported by the caller rather than raising.
    """
    op = spec.get("paths", {}).get(tmpl, {}).get("get")
    if not isinstance(op, dict):
        return []
    for p in op.get("parameters", []):
        p = deref(p)
        if p.get("name") == "window":
            return p.get("schema", {}).get("enum", [])
    return []


# ---------------------------------------------------------------- preflight
status, raw, _ = request("GET", f"{BASE}/health")
if status != 200:
    print(f"cannot reach {BASE}/health: {raw[:200]!r}", file=sys.stderr)
    sys.exit(2)

# ---------------------------------------------------------------- discovery
def first_id(payload):
    if isinstance(payload, list):
        for item in payload:
            if isinstance(item, dict) and item.get("id"):
                return item["id"]
    return None


sid = first_id(json.loads(request("GET", f"{BASE}/api/sessions?limit=25")[1]))
rid = first_id(json.loads(request("GET", f"{BASE}/api/repos")[1]))

# ---------------------------------------------------------------- GET sweep
for tmpl, url, note in [
    ("/health", f"{BASE}/health", ""),
    ("/api/version", f"{BASE}/api/version", ""),
    ("/api/sessions", f"{BASE}/api/sessions?limit=5", ""),
    ("/api/sessions", f"{BASE}/api/sessions?group=activity", "[grouped]"),
    ("/api/repos", f"{BASE}/api/repos", ""),
    ("/api/workflows", f"{BASE}/api/workflows", ""),
    ("/api/storage", f"{BASE}/api/storage", ""),
    ("/api/settings", f"{BASE}/api/settings", ""),
    ("/api/pricing", f"{BASE}/api/pricing", ""),
    ("/api/search", f"{BASE}/api/search?q=error&limit=5", ""),
    ("/api/search", f"{BASE}/api/search", "[no q]"),
    ("/api/search/full", f"{BASE}/api/search/full?q=error&limit=5", ""),
    ("/api/search/combined", f"{BASE}/api/search/combined?q=error&limit=5", ""),
    ("/api/skills/sessions", f"{BASE}/api/skills/sessions", ""),
]:
    check("GET", tmpl, url, note=note)

# Window vocabularies, driven off each operation's own declared enum so this
# asserts "handler honours its spec" rather than any hardcoded expectation.
ALL_WINDOWS = ["all", "today", "week", "month", "24h", "7d", "30d"]
for tmpl in ["/api/stats", "/api/stats/trends", "/api/stats/tools"]:
    declared = window_enum(tmpl)
    if not declared:
        add("ERROR", f"GET {tmpl}",
            "no window parameter enum declared in spec (is the path documented?)")
        continue
    for w in declared:
        check("GET", tmpl, f"{BASE}{tmpl}?window={w}", note=f"[{w}]")
    # A token this operation does not declare must be rejected, and the spec
    # must document that rejection.
    for w in [x for x in ALL_WINDOWS if x not in declared]:
        check("GET", tmpl, f"{BASE}{tmpl}?window={w}", expect="400", note=f"[{w} undeclared]")
    check("GET", tmpl, f"{BASE}{tmpl}", note="[no window]")
    check("GET", tmpl, f"{BASE}{tmpl}?window=nonsense", expect="400", note="[invalid]")

# ------------------------------------------------------- id-scoped endpoints
if sid:
    check("GET", "/api/sessions/{id}", f"{BASE}/api/sessions/{sid}")
    check("GET", "/api/sessions/{id}/events", f"{BASE}/api/sessions/{sid}/events?limit=5")
    check("GET", "/api/sessions/{id}/autopsy", f"{BASE}/api/sessions/{sid}/autopsy")
    check("GET", "/api/sessions/{id}/replay", f"{BASE}/api/sessions/{sid}/replay?limit=5")
else:
    add("SKIP", "session-scoped endpoints",
        "no sessions in the database — /api/sessions/{id}[/events|/autopsy|/replay] "
        "response schemas were not validated")

if rid:
    # Repo ids are canonical paths containing '/', so they must be encoded.
    enc = urllib.parse.quote(rid, safe="")
    check("GET", "/api/repos/{id}/stats", f"{BASE}/api/repos/{enc}/stats")
    check("GET", "/api/repos/{id}/sessions", f"{BASE}/api/repos/{enc}/sessions?limit=5")
    if "/" in rid:
        st, _, _ = request("GET", f"{BASE}/api/repos/{rid}/stats")
        if st != 200:
            repo_param = spec.get("components", {}).get("parameters", {}).get("RepoId", {})
            texts = (
                deref(repo_param).get("description", "") + spec["info"]["description"]
            ).lower()
            if "percent-encode" not in texts:
                add("DRIFT", "GET /api/repos/{id}/stats",
                    f"an unencoded repo id yields HTTP {st}, but neither the RepoId "
                    f"parameter nor the routing note tells callers to encode it")
else:
    add("SKIP", "repo-scoped endpoints",
        "no repos in the database — /api/repos/{id}/[stats|sessions] response "
        "schemas were not validated")

# ------------------------------------------------------- documented failures
check("GET", "/api/sessions/{id}", f"{BASE}/api/sessions/does-not-exist",
      expect="404", note="[unknown id]")
for tmpl in ["/api/repos/{id}/stats", "/api/repos/{id}/sessions"]:
    check("GET", tmpl, f"{BASE}{tmpl.replace('{id}', 'no-such-repo')}",
          expect="404", note="[unknown repo]")
if "not 404 for an unknown repo id" in spec["info"]["description"]:
    add("ERROR", "spec info.description",
        "routing note still claims the per-repo endpoints do not 404 for an "
        "unknown repo id, but they do")

# ------------------------------------ mutating endpoints (isolated DB only!)
check("PUT", "/api/settings/{key}", f"{BASE}/api/settings/retention_hot_days",
      body={"value": "45"}, note="[valid]")
check("PUT", "/api/settings/{key}", f"{BASE}/api/settings/preview_max_length",
      body={"value": "300"}, note="[valid]")
for key, val, why in [
    ("backfill_v013_done", "1", "existing but not allowlisted"),
    ("totally_made_up_key", "1", "unknown key"),
    ("retention_hot_days", "0", "non-positive"),
    ("retention_hot_days", "abc", "non-numeric"),
    ("preview_max_length", "10", "below documented min 50"),
    ("preview_max_length", "5000", "above documented max 2000"),
]:
    check("PUT", "/api/settings/{key}", f"{BASE}/api/settings/{key}",
          body={"value": val}, expect="400", note=f"[{why}]")

after = json.loads(request("GET", f"{BASE}/api/settings")[1])
for k, want in [("retention_hot_days", "45"), ("preview_max_length", "300")]:
    if str(after.get(k)) != want:
        add("ERROR", "PUT /api/settings/{key}",
            f"wrote {k}={want} but GET /api/settings reports {after.get(k)!r}")

check("PUT", "/api/pricing/{model_prefix}", f"{BASE}/api/pricing/zz-spec-test",
      body={"input_per_mtok": 1.0, "output_per_mtok": 2.0,
            "cache_read_per_mtok": 0.1, "cache_create_per_mtok": 1.25},
      note="[valid upsert]")
pricing = json.loads(request("GET", f"{BASE}/api/pricing")[1])
got = pricing.get("zz-spec-test") if isinstance(pricing, dict) else None
if got is None:
    add("ERROR", "PUT /api/pricing/{model_prefix}",
        "upserted zz-spec-test but it is absent from GET /api/pricing")
elif got.get("output_per_mtok") != 2.0:
    add("ERROR", "PUT /api/pricing/{model_prefix}",
        f"upserted output_per_mtok=2.0 but GET reports {got.get('output_per_mtok')!r}")

check("DELETE", "/api/cache/repos", f"{BASE}/api/cache/repos")

# ------------------------------------------------------------- method guard
st, _, hdrs = request("POST", f"{BASE}/api/sessions")
if st != 405:
    add("ERROR", "POST /api/sessions",
        f"expected 405 per the spec's routing note, got {st}")
elif not hdrs.get("Allow"):
    add("ERROR", "POST /api/sessions",
        "405 returned without an Allow header, which the routing note promises")

# ------------------------------------------------- route coverage vs main.go
registered = set()
for m in re.finditer(r'mux\.HandleFunc\("(GET|PUT|POST|DELETE) ([^"]+)"', open(MAIN_GO).read()):
    method, path = m.group(1), m.group(2)
    if path.startswith("/api/") or path in ("/health", "/ws"):
        registered.add((method, path))
for method, path in sorted(registered):
    if path in ("/api", "/api/openapi.yaml"):  # Swagger UI, not part of the API
        continue
    if path not in spec["paths"]:
        add("DRIFT", f"{method} {path}",
            "route is registered in main.go but absent from openapi.yaml")
    elif method.lower() not in spec["paths"][path]:
        add("DRIFT", f"{method} {path}",
            f"route registered for {method} but spec documents only "
            f"{[k for k in spec['paths'][path] if k != 'parameters']}")

# --------------------------------------------------------- spec self-checks
declared = spec["info"]["version"]
live = json.loads(request("GET", f"{BASE}/api/version")[1]).get("version")
if not re.fullmatch(r"\d+\.\d+\.\d+", str(live or "")):
    # Dev/CI builds report "dev" or a tag; only compare against a real version.
    add("SKIP", "spec info.version",
        f"server reports {live!r}, not a release version — version sync unchecked")
elif declared != live:
    add("DRIFT", "spec info.version",
        f"spec says {declared!r} but server reports {live!r}")

# ---------------------------------------------------------------- reporting
errors = [r for r in results if r[0] == "ERROR"]
drift = [r for r in results if r[0] == "DRIFT"]
skips = [r for r in results if r[0] == "SKIP"]

for label, rows in (("ERRORS", errors), ("DRIFT", drift), ("SKIPPED", skips)):
    print(f"\n{'=' * 72}\n{label} ({len(rows)})\n{'=' * 72}")
    for _, ep, msg in rows:
        print(f"  [{ep}]\n      {msg}")

print(f"\n{len(errors)} errors, {len(drift)} drift, {len(skips)} skipped")
sys.exit(1 if (errors or drift) else 0)
