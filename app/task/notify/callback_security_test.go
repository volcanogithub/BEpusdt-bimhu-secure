package notify

import (
 "context"
 "crypto/tls"
 "net"
 "net/http"
 "net/netip"
 "net/url"
 "strings"
 "testing"
 "time"

 "github.com/v03413/bepusdt/app/model"
 "github.com/v03413/bepusdt/app/security"
)

// Test-only routing: production never has an allow-private switch.
type callbackTestResolver struct{}
func (callbackTestResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil }
func callbackTestURL(raw string) string { return strings.Replace(raw, "127.0.0.1", "93.184.216.34", 1) }
func callbackTestClient(base *http.Client) *http.Client {
 c := *base
 tr, ok := base.Transport.(*http.Transport)
 if !ok { tr = http.DefaultTransport.(*http.Transport) }
 // Tests use an isolated TLS server and a test-only TLS trust configuration.
 tr = tr.Clone()
 if tr.TLSClientConfig == nil { tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} } else { tr.TLSClientConfig = tr.TLSClientConfig.Clone(); tr.TLSClientConfig.InsecureSkipVerify = true }
 c.Transport = security.NewTransport(tr, callbackTestResolver{}, func(ctx context.Context, network, address string) (net.Conn, error) {
  _, port, err := net.SplitHostPort(address); if err != nil { return nil, err }
  return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
 })
 return &c
}

func TestB24RejectedCallbackKeepsSuccessAndRetries(t *testing.T) {
 db := outboxDB(t)
 n := queuedOrder(t, db, "https://169.254.169.254/latest/meta-data")
 err := ProcessOne(t.Context(), "ssrf-worker", nil, time.Now(), time.Second)
 if err == nil || !strings.Contains(err.Error(), "callback rejected") { t.Fatalf("expected destination rejection: %v", err) }
 var row model.NotificationDelivery
 db.First(&row, n.ID)
 if row.Status != model.NotificationStatusRetry || !strings.Contains(row.LastError, "callback rejected") { t.Fatalf("failure not persisted: %#v", row) }
 var order model.Order
 db.First(&order, n.OrderID)
 if order.Status != model.OrderStatusSuccess { t.Fatal("callback changed payment status") }
}

func TestB24AllCallbackPathsRejectPrivateDestination(t *testing.T) {
 db := outboxDB(t)
 initNotifyTestLog(t)
 o := newWaitingOrder("https://127.0.0.1/callback")
 if err := db.Create(&o).Error; err != nil { t.Fatal(err) }
 for _, kind := range []string{model.OrderApiTypeEpay, model.OrderApiTypeEpusdt} {
  o.ApiType = kind
  if err := deliverStableEvent(t.Context(), nil, o, "stable-id"); err == nil { t.Fatal("durable callback allowed private URL") }
 }
 if err := deliverBepusdtStatusUpdate(db, nil, "test-token", o); err == nil { t.Fatal("status callback allowed private URL") }
 // URL parsing is deliberately not a network operation.
 if _, err := url.Parse(o.NotifyUrl); err != nil { t.Fatal(err) }
}
