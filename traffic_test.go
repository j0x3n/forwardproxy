package forwardproxy

import (
 "bytes"
 "context"
 "io"
 "net/http/httptest"
 "sync"
 "testing"

 "github.com/caddyserver/caddy/v2"
)
func TestTrafficUnlimitedAndAuthenticated(t *testing.T) {
 h := &Handler{AuthCredentials:[][]byte{EncodeAuthCredentials("vip","test-only")},BandwidthNamespace:"traffic-test"}
 r := httptest.NewRequest("CONNECT","https://target.invalid",nil)
 repl := caddy.NewReplacer()
 r = r.WithContext(context.WithValue(r.Context(),caddy.ReplacerCtxKey,repl))
 if h.requestBandwidth(r) != nil { t.Fatal("unauthenticated request metered") }
 repl.Set("http.auth.user.id","vip")
 a := h.requestBandwidth(r)
 if a == nil || a.stats == nil || a.upload != nil || a.download != nil { t.Fatal("unlimited account missing stats or is limited") }
 body := &bandwidthBody{ReadCloser:io.NopCloser(bytes.NewReader([]byte("hello"))),ctx:context.Background(),counter:&a.stats.upload}
 if _,err := io.ReadAll(body);err != nil { t.Fatal(err) }
 if a.stats.upload.Load() != 5 { t.Fatal("incorrect byte count") }
 key := bandwidthKey{"traffic-test","vip",BandwidthPolicy{1,1}}
 if sharedBandwidth(key).stats != a.stats { t.Fatal("rate change splits traffic counters") }
}
func TestTrafficConcurrentAndAdmin(t *testing.T) {
 s := sharedTraffic("traffic-concurrent","user")
 var wg sync.WaitGroup
 for i:=0;i<10;i++ { wg.Add(1);go func(){ defer wg.Done();for j:=0;j<1000;j++ { s.download.Add(1) } }() }
 wg.Wait()
 if s.download.Load()!=10000 { t.Fatal("lost increments") }
 w:=httptest.NewRecorder()
 if err:=(trafficAdmin{}).handle(w,httptest.NewRequest("GET","/forward_proxy/traffic",nil));err!=nil { t.Fatal(err) }
 if w.Code!=200 || !bytes.Contains(w.Body.Bytes(),[]byte("traffic-concurrent")) { t.Fatal("missing stats") }
 if err:=(trafficAdmin{}).handle(httptest.NewRecorder(),httptest.NewRequest("POST","/forward_proxy/traffic",nil));err==nil { t.Fatal("write method accepted") }
}
