# Security

## Threat Model

claude-monitor is a **local-only** development tool. It is designed to run on
`localhost` and is not hardened for exposure to untrusted networks.

## Key Points

- **No authentication.** All HTTP and WebSocket endpoints are open. Anyone who
  can reach the port can view session data (costs, status, file paths, etc.).
- **Default bind: localhost only.** The server listens on `127.0.0.1:7700` by
  default, which limits access to the local machine.
- **`--broadcast` flag (0.0.0.0).** When enabled, the server binds to all
  interfaces. Anyone on the same network can connect and view session data.
- **WebSocket origin check.** Connections are accepted only from `same-origin`
  and `localhost` origins, which mitigates drive-by browser attacks but is not
  a substitute for real authentication.

## Recommendations

1. **Do not expose to public networks.** There is no auth layer; treat the
   dashboard as you would any other local dev server.
2. **Prefer localhost.** Avoid `--broadcast` unless you specifically need LAN
   access and trust everyone on that network.
3. **Use an SSH tunnel for remote access.**
   ```
   ssh -L 7700:localhost:7700 your-server
   ```
   Then open `http://localhost:7700` locally. The traffic is encrypted and
   access is limited to your SSH credentials.

## Reporting Issues

If you find a security-relevant bug, please open a GitHub issue at
<https://github.com/Zxela/claude-monitor/issues>.
