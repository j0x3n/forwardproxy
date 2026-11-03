package forwardproxy

import (
 "context"
 "encoding/base64"
 "fmt"
 "io"
 "net"
 "net/http"
 "strings"
 "sync"
 "sync/atomic"

 "github.com/caddyserver/caddy/v2"
 "golang.org/x/time/rate"
)

const bandwidthChunk = 32 * 1024
const maxBandwidthRate int64 = 1 << 50

// BandwidthPolicy uses payload bytes/second, upload then download. Zero is unlimited.
type BandwidthPolicy struct {
 Upload int64 `json:"upload"`
 Download int64 `json:"download"`
}

type accountBandwidth struct { upload, download *rate.Limiter; stats *accountTraffic }
type bandwidthKey struct { namespace, user string; policy BandwidthPolicy }

// Process-lifetime registry preserves same-policy buckets across config reloads.
// Entries contain configured usernames, never passwords or client-controlled names.
// A changed policy has a different bucket: existing tunnels retain their old policy.
// Apply changed limits with a controlled process restart, not a live config reload.
var bandwidthRegistry = struct {
 sync.Mutex
 accounts map[bandwidthKey]*accountBandwidth
}{accounts: make(map[bandwidthKey]*accountBandwidth)}

func newBandwidthLimiter(n int64) *rate.Limiter {
 if n == 0 { return nil }
 return rate.NewLimiter(rate.Limit(n), bandwidthChunk)
}

func sharedBandwidth(key bandwidthKey) *accountBandwidth {
 bandwidthRegistry.Lock()
 defer bandwidthRegistry.Unlock()
 if v := bandwidthRegistry.accounts[key]; v != nil { return v }
 v := &accountBandwidth{upload: newBandwidthLimiter(key.policy.Upload), download: newBandwidthLimiter(key.policy.Download), stats: sharedTraffic(key.namespace, key.user)}
 bandwidthRegistry.accounts[key] = v
 return v
}

func (h *Handler) validateBandwidth() error {
 users := make(map[string]bool)
 for _, encoded := range h.AuthCredentials {
  b, err := base64.StdEncoding.DecodeString(string(encoded))
  if err != nil { return fmt.Errorf("invalid configured auth encoding") }
  user, _, ok := strings.Cut(string(b), ":")
  if ok { users[user] = true }
 }
 for user, policy := range h.UserBandwidth {
  if !users[user] { return fmt.Errorf("user_bandwidth: unknown authenticated user %q", user) }
  if policy.Upload < 0 || policy.Download < 0 || policy.Upload > maxBandwidthRate || policy.Download > maxBandwidthRate {
   return fmt.Errorf("user_bandwidth: invalid rate for %q", user)
  }
 }
 return nil
}

// Only called on the authenticated proxy path, never on fallback website requests.
func (h *Handler) requestBandwidth(r *http.Request) *accountBandwidth {
 if len(h.AuthCredentials) == 0 { return nil }
 repl, ok := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
 if !ok { return nil }
 value, ok := repl.Get("http.auth.user.id")
 if !ok { return nil }
 user, ok := value.(string)
 if !ok { return nil }
 policy := h.UserBandwidth[user]
 return sharedBandwidth(bandwidthKey{h.BandwidthNamespace, user, policy})
}

func bandwidthRead(ctx context.Context, src io.Reader, p []byte, limiter *rate.Limiter) (int, error) {
 if limiter == nil { return src.Read(p) }
 if len(p) > bandwidthChunk { p = p[:bandwidthChunk] }
 n, err := src.Read(p)
 if n > 0 {
  if waitErr := limiter.WaitN(ctx, n); waitErr != nil { return 0, waitErr }
 }
 return n, err
}

func bandwidthWrite(ctx context.Context, dst io.Writer, p []byte, limiter *rate.Limiter) (int, error) {
 if limiter == nil { return dst.Write(p) }
 total := 0
 for len(p) > 0 {
  size := len(p); if size > bandwidthChunk { size = bandwidthChunk }
  if err := limiter.WaitN(ctx, size); err != nil { return total, err }
  n, err := dst.Write(p[:size]); total += n
  if err != nil { return total, err }
  if n != size { return total, io.ErrShortWrite }
  p = p[size:]
 }
 return total, nil
}

type bandwidthConn struct {
 net.Conn
 ctx context.Context
 cancel context.CancelFunc
 limits *accountBandwidth
}
func (c *bandwidthConn) Read(p []byte) (int, error) {
 n, err := bandwidthRead(c.ctx, c.Conn, p, c.limits.download)
 if c.limits.stats != nil { c.limits.stats.download.Add(uint64(n)) }
 return n, err
}
func (c *bandwidthConn) Write(p []byte) (int, error) {
 n, err := bandwidthWrite(c.ctx, c.Conn, p, c.limits.upload)
 if c.limits.stats != nil { c.limits.stats.upload.Add(uint64(n)) }
 return n, err
}
func (c *bandwidthConn) Close() error { c.cancel(); return c.Conn.Close() }
func (c *bandwidthConn) CloseWrite() error {
 if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok { return cw.CloseWrite() }
 return nil
}

type bandwidthBody struct {
 io.ReadCloser
 ctx context.Context
 limiter *rate.Limiter
 counter *atomic.Uint64
}
func (b *bandwidthBody) Read(p []byte) (int, error) {
 n, err := bandwidthRead(b.ctx, b.ReadCloser, p, b.limiter)
 if b.counter != nil { b.counter.Add(uint64(n)) }
 return n, err
}
