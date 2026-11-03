package forwardproxy

import (
 "bytes"
 "context"
 "errors"
 "io"
 "sync"
 "testing"
 "time"

 "github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func TestBandwidthConfig(t *testing.T) {
 var h Handler
 err := h.UnmarshalCaddyfile(caddyfile.NewTestDispenser(`forward_proxy {
 basic_auth test pass
 user_bandwidth test 625000 625000
 }`))
 if err != nil { t.Fatal(err) }
 if err = h.validateBandwidth(); err != nil { t.Fatal(err) }
 if h.UserBandwidth["test"].Upload != 625000 { t.Fatal("wrong units") }
 for _, input := range []string{
  "forward_proxy {\n user_bandwidth nobody 1 1\n}",
  "forward_proxy {\n basic_auth test pass\n user_bandwidth test -1 1\n}",
  "forward_proxy {\n basic_auth test pass\n user_bandwidth test 1 1\n user_bandwidth test 2 2\n}",
 } {
  var invalid Handler
  err := invalid.UnmarshalCaddyfile(caddyfile.NewTestDispenser(input))
  if err == nil { err = invalid.validateBandwidth() }
  if err == nil { t.Fatalf("accepted invalid config %s", input) }
 }
}

func TestBandwidthSharedAndIndependent(t *testing.T) {
 key := bandwidthKey{"test-sharing", "alice", BandwidthPolicy{625000,625000}}
 a := sharedBandwidth(key)
 if a != sharedBandwidth(key) { t.Fatal("reload/connection buckets differ") }
 if a.upload == a.download { t.Fatal("directions share bucket") }
 key.user = "bob"
 if a == sharedBandwidth(key) { t.Fatal("accounts share bucket") }
 if newBandwidthLimiter(0) != nil { t.Fatal("zero must be unlimited") }
}

func TestBandwidthAggregate(t *testing.T) {
 // Four concurrent streams share a 625000 byte/s account, not 4x625000.
 limiter := newBandwidthLimiter(625000)
 payload := bytes.Repeat([]byte{42}, 156250)
 start := time.Now()
 var wg sync.WaitGroup
 for i := 0; i < 4; i++ {
  wg.Add(1)
  go func() {
   defer wg.Done()
   if n, err := bandwidthWrite(context.Background(), io.Discard, payload, limiter); err != nil || n != len(payload) {
    t.Errorf("write: %d %v", n, err)
   }
  }()
 }
 wg.Wait()
 elapsed := time.Since(start)
 if elapsed < 850*time.Millisecond || elapsed > 5*time.Second { t.Fatalf("aggregate duration %s", elapsed) }
}

func TestBandwidthReadAndCancel(t *testing.T) {
 payload := bytes.Repeat([]byte{7}, 125000)
 limiter := newBandwidthLimiter(125000)
 start := time.Now()
 body := &bandwidthBody{ReadCloser: io.NopCloser(bytes.NewReader(payload)), ctx: context.Background(), limiter: limiter}
 result, err := io.ReadAll(body)
 if err != nil || !bytes.Equal(payload, result) { t.Fatal("read corrupted", err) }
 if time.Since(start) < 650*time.Millisecond { t.Fatal("download not limited") }
 ctx, cancel := context.WithCancel(context.Background()); cancel()
 limiter = newBandwidthLimiter(1)
 limiter.AllowN(time.Now(), bandwidthChunk)
 _, err = bandwidthWrite(ctx, io.Discard, []byte{1}, limiter)
 if !errors.Is(err, context.Canceled) { t.Fatalf("cancel: %v", err) }
 result = make([]byte,len(payload))
 n, err := bandwidthRead(context.Background(), bytes.NewReader(payload), result, nil)
 if err != nil || n != len(payload) { t.Fatal("unlimited read failed") }
}

type shortBandwidthWriter struct{}
func (shortBandwidthWriter) Write(p []byte) (int,error) { return len(p)/2,nil }
func TestBandwidthShortWrite(t *testing.T) {
 _, err := bandwidthWrite(context.Background(), shortBandwidthWriter{}, []byte("abcd"), newBandwidthLimiter(625000))
 if !errors.Is(err, io.ErrShortWrite) { t.Fatal(err) }
}
