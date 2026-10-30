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
- **Host check.** Requests are refused (403) unless the `Host` header is
  `localhost`, an IP address, or a name passed with `--allowed-host`. This
  blocks DNS-rebinding attacks, where a website re-points its own hostname at
  127.0.0.1 to read the API from the victim's browser.
- **WebSocket origin check.** Connections are accepted only from the same
  origin as the `Host` header. Neither check is a substitute for real
  authentication.

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
