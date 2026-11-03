package forwardproxy

import (
 "encoding/json"
 "fmt"
 "net/http"
 "sort"
 "sync"
 "sync/atomic"
 "time"

 "github.com/caddyserver/caddy/v2"
)

type accountTraffic struct {
 upload atomic.Uint64
 download atomic.Uint64
 active atomic.Int64
}
type trafficKey struct { namespace, user string }
var trafficRegistry = struct {
 sync.Mutex
 accounts map[trafficKey]*accountTraffic
}{accounts: make(map[trafficKey]*accountTraffic)}
var trafficEpoch = fmt.Sprintf("%d", time.Now().UnixNano())

func sharedTraffic(namespace, user string) *accountTraffic {
 trafficRegistry.Lock()
 defer trafficRegistry.Unlock()
 key := trafficKey{namespace,user}
 if s := trafficRegistry.accounts[key]; s != nil { return s }
 s := new(accountTraffic)
 trafficRegistry.accounts[key] = s
 return s
}

type trafficRow struct {
 Namespace string `json:"namespace"`
 User string `json:"user"`
 Upload uint64 `json:"upload_bytes"`
 Download uint64 `json:"download_bytes"`
 Active int64 `json:"active_tunnels"`
}
func trafficSnapshot() []trafficRow {
 trafficRegistry.Lock()
 defer trafficRegistry.Unlock()
 rows := make([]trafficRow,0,len(trafficRegistry.accounts))
 for k,s := range trafficRegistry.accounts {
  rows = append(rows,trafficRow{k.namespace,k.user,s.upload.Load(),s.download.Load(),s.active.Load()})
 }
 sort.Slice(rows,func(i,j int) bool {
  if rows[i].Namespace != rows[j].Namespace { return rows[i].Namespace < rows[j].Namespace }
  return rows[i].User < rows[j].User
 })
 return rows
}

type trafficAdmin struct{}
func init() { caddy.RegisterModule(trafficAdmin{}) }
func (trafficAdmin) CaddyModule() caddy.ModuleInfo {
 return caddy.ModuleInfo{ID:"admin.api.forward_proxy_traffic",New:func() caddy.Module { return new(trafficAdmin) }}
}
func (a trafficAdmin) Routes() []caddy.AdminRoute {
 return []caddy.AdminRoute{{Pattern:"/forward_proxy/traffic",Handler:caddy.AdminHandlerFunc(a.handle)}}
}
func (trafficAdmin) handle(w http.ResponseWriter,r *http.Request) error {
 if r.Method != http.MethodGet { return caddy.APIError{HTTPStatus:405,Err:fmt.Errorf("method not allowed")} }
 w.Header().Set("Content-Type","application/json")
 w.Header().Set("Cache-Control","no-store")
 return json.NewEncoder(w).Encode(struct {
  Epoch string `json:"epoch"`
  Time int64 `json:"time_unix"`
  Accounts []trafficRow `json:"accounts"`
 }{trafficEpoch,time.Now().Unix(),trafficSnapshot()})
}
