// Package security enforces the server-side callback network boundary.
package security

import (
 "context"
 "errors"
 "net"
 "net/http"
 "net/netip"
 "net/url"
 "strconv"
 "strings"
 "time"
)

// Resolver and DialFunc are trusted dependencies, not URL-controlled configuration.
type Resolver interface { LookupNetIP(context.Context, string, string) ([]netip.Addr, error) }
type DialFunc func(context.Context, string, string) (net.Conn, error)

var denied = []netip.Prefix{
 netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
 netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
 netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
 netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
 netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("198.18.0.0/15"),
 netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
 netip.MustParsePrefix("224.0.0.0/3"), netip.MustParsePrefix("2001::/23"),
 netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

func publicIP(ip netip.Addr) bool {
 ip = ip.Unmap()
 if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() { return false }
 // Only native global IPv6; reject NAT64, IPv4 translation and metadata ULA.
 if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) { return false }
 for _, block := range denied { if block.Contains(ip) { return false } }
 return true
}

// ValidateURL performs admission checks without initiating network traffic.
// DNS is checked again by the transport at every request and connection.
func ValidateURL(raw string) error {
 u, err := url.ParseRequestURI(raw)
 if err != nil || u == nil || u.Scheme != "https" || u.Host == "" || u.Opaque != "" || u.User != nil || u.Fragment != "" {
  return errors.New("callback rejected: absolute HTTPS URL without userinfo or fragment required")
 }
 host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
 if host == "" || strings.Contains(host, "%") || host == "localhost" || host == "localhost.localdomain" || strings.HasSuffix(host, ".localhost") || !strings.Contains(host, ".") && !strings.Contains(host, ":") {
  return errors.New("callback rejected: invalid or local hostname")
 }
 if port := u.Port(); port != "" { n, e := strconv.Atoi(port); if e != nil || n < 1 || n > 65535 { return errors.New("callback rejected: invalid port") } }
 if ip, e := netip.ParseAddr(host); e == nil {
  if !publicIP(ip) { return errors.New("callback rejected: non-public IP address") }
 } else {
  // Reject ambiguous numeric representations, Unicode and malformed DNS labels.
  numeric := true
  for _, c := range host { if c != '.' && (c < '0' || c > '9') { numeric = false } }
  if numeric || len(host) > 253 { return errors.New("callback rejected: invalid DNS hostname") }
  for _, label := range strings.Split(host, ".") {
   if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' { return errors.New("callback rejected: invalid DNS hostname") }
   for _, c := range label { if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') { return errors.New("callback rejected: invalid DNS hostname") } }
  }
 }
 return nil
}

func resolve(ctx context.Context, r Resolver, host string) ([]netip.Addr, error) {
 host = strings.TrimSuffix(host, ".")
 var ips []netip.Addr
 if ip, err := netip.ParseAddr(host); err == nil { ips = []netip.Addr{ip} } else {
  var err error
  ips, err = r.LookupNetIP(ctx, "ip", host)
  if err != nil { return nil, errors.New("callback rejected: DNS lookup failed") }
 }
 if len(ips) == 0 { return nil, errors.New("callback rejected: empty DNS answer") }
 for _, ip := range ips { if !publicIP(ip) { return nil, errors.New("callback rejected: DNS contains non-public address") } }
 return ips, nil
}

type callbackTransport struct { inner *http.Transport; resolver Resolver }

func (t *callbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
 if err := ValidateURL(req.URL.String()); err != nil { return nil, err }
 if _, err := resolve(req.Context(), t.resolver, req.URL.Hostname()); err != nil { return nil, err }
 return t.inner.RoundTrip(req)
}

// NewTransport constructs a guarded transport. Resolver/dial injection is for
// trusted embedding/tests only. Production callers pass nil for both.
// No environment proxy, custom TLS dial hook, redirect, or second DNS dial.
func NewTransport(base *http.Transport, r Resolver, dial DialFunc) http.RoundTripper {
 if r == nil { r = net.DefaultResolver }
 if dial == nil { d := &net.Dialer{Timeout: 5*time.Second}; dial = d.DialContext }
 if base == nil { base = http.DefaultTransport.(*http.Transport) }
 tr := base.Clone()
 tr.Proxy = nil
 tr.DialTLSContext = nil
 tr.DialTLS = nil
 tr.Dial = nil
 tr.DisableKeepAlives = true
 tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
  host, port, err := net.SplitHostPort(address); if err != nil { return nil, errors.New("callback rejected: invalid dial address") }
  ips, err := resolve(ctx, r, host); if err != nil { return nil, err }
  var last error
  for _, ip := range ips { conn, e := dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port)); if e == nil { return conn, nil }; last = e }
  return nil, last
 }
 return &callbackTransport{inner: tr, resolver: r}
}

// Client never trusts an arbitrary supplied RoundTripper to enforce the boundary.
// Existing timeout is preserved; only a transport produced above is reusable.
func Client(base *http.Client) *http.Client {
 timeout := 5*time.Second
 var rt http.RoundTripper
 if base != nil {
  if base.Timeout > 0 { timeout = base.Timeout }
  if guarded, ok := base.Transport.(*callbackTransport); ok { rt = guarded }
 }
 if rt == nil { rt = NewTransport(nil, nil, nil) }
 return &http.Client{Timeout: timeout, Transport: rt, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
