# Per-account bandwidth (experimental)

New forward_proxy directives:

```
forward_proxy {
    basic_auth alice EXAMPLE-NOT-A-REAL-PASSWORD
    basic_auth unrestricted ANOTHER-EXAMPLE
    bandwidth_namespace primary
    user_bandwidth alice 625000 625000
}
```

`user_bandwidth USER UPLOAD_BYTES_PER_SECOND DOWNLOAD_BYTES_PER_SECOND`.
625000 bytes/s = 5 decimal Mbps. Zero or an omitted user means unlimited.
Each direction has its own 32 KiB burst token bucket. All streams for the same
namespace, authenticated username and rate policy share a process-wide bucket,
including streams from identical configurations after reload. Different users
have independent buckets. Use distinct namespaces for unrelated proxy sites.
Only successfully authenticated proxy requests are metered, not fallback sites.
CONNECT meters the target socket, preserving Naive padding and HTTP flush logic.
Non-CONNECT proxy request/response bodies are also metered.

Limitations before production approval:
- A changed rate policy creates a new bucket; existing tunnels retain old rates.
  Applying rate changes requires a controlled process restart, not reload.
- The process-lifetime registry retains entries until restart; suitable for a
  small static account set, not unbounded account churn.
- Limits meter payload, excluding transport/padding overhead, not exact NIC bps.
- Initial automated tests cover parsing, shared state, concurrent aggregate
  writes, reads, cancellation and short writes; full real-Naive, HTTP/2,
  half-close, disconnect/deadline and streaming integration remains required.
- net.Conn deadlines do not currently interrupt token waits independently of
  cancellation. HTTP/1 hijacked streams use a tunnel-scoped context.

Do not deploy solely because compilation succeeds. Keep the previous executable
and configuration. Replacing the executable on disk does not upgrade a running
process; controlled process switching and connection reconnection are required.

No real credentials, certificates or production configuration belong in CI.

## Account traffic (new; not deployed)

GET `/forward_proxy/traffic` on the Caddy **admin endpoint** returns process epoch,
namespace/username, upload/download payload byte counters and active CONNECT
handlers. Unlimited authenticated accounts are metered too. Counters survive
same-process reloads and rate policy changes, not process restarts. Active tunnels
are not device/user counts. Reads count payload successfully returned by the
metered read, not final delivery acknowledgments. Writes count accepted bytes.
No passwords, Authorization headers, client IPs or destinations are exported.
Keep admin loopback-only; never expose the admin API through a public proxy.

`ci/traffic_collector.py --db /protected/path/traffic.sqlite3` is a prepared local
collector; it polls every 10s and persists daily totals (Asia/Shanghai) and recent
rates. It is not installed/enabled automatically. Epoch reset and duplicate
sampling are tested. Traffic between the last poll and process death can be lost;
the first snapshot includes bytes since process start. Each delta is attributed
to its collection day, so midnight boundary attribution has poll-interval error.
Counters include only authenticated accounts used since process start. Historical
activity before feature activation cannot be recovered. Protected web UI and
collector service integration are still pending; existing audit page is untouched.
