package security

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(c context.Context, n, h string) ([]netip.Addr, error) {
	return f(c, n, h)
}
func answer(values ...string) Resolver {
	return resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		ips := make([]netip.Addr, 0, len(values))
		for _, s := range values {
			ips = append(ips, netip.MustParseAddr(s))
		}
		return ips, nil
	})
}
func TestB24URLAdmission(t *testing.T) {
	if err := ValidateURL("https://example.com/callback"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"http://example.com", "file:///etc/passwd", "ftp://example.com", "gopher://example.com",
		"https://localhost", "https://localhost.localdomain", "https://LOCALHOST.",
		"https://127.0.0.1", "https://10.1.2.3", "https://172.16.0.1", "https://192.168.1.1",
		"https://169.254.169.254", "https://100.100.100.200", "https://0.0.0.0",
		"https://[::1]", "https://[fc00::1]", "https://[fe80::1]", "https://[fd00:ec2::254]",
		"https://[::ffff:127.0.0.1]", "https://[64:ff9b::a9fe:a9fe]", "https://[2002:7f00:1::]",
		"https://2130706433", "https://127.1", "***example.com", "https://example.com:0",
	} {
		if err := ValidateURL(raw); err == nil {
			t.Errorf("allowed %s", raw)
		}
	}
}
func TestB24DNSRejectsEveryNonPublicAnswer(t *testing.T) {
	for _, r := range []Resolver{answer("127.0.0.1"), answer("93.184.216.34", "10.0.0.1"), answer("::ffff:127.0.0.1"), answer(), resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return nil, errors.New("timeout") })} {
		var calls atomic.Int32
		c := Client(&http.Client{Transport: NewTransport(nil, r, func(context.Context, string, string) (net.Conn, error) {
			calls.Add(1)
			return nil, errors.New("must not dial")
		})})
		if _, err := c.Get("https://public-domain.example/callback"); err == nil {
			t.Fatal("expected rejection")
		}
		if calls.Load() != 0 {
			t.Fatal("rejected DNS reached dial")
		}
	}
}
func TestB24RebindingRejectedAtDial(t *testing.T) {
	var lookups, dials atomic.Int32
	r := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		if lookups.Add(1) == 1 {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	c := Client(&http.Client{Transport: NewTransport(nil, r, func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("unexpected dial")
	})})
	if _, err := c.Get("https://public-domain.example"); err == nil {
		t.Fatal("expected rebinding rejection")
	}
	if dials.Load() != 0 {
		t.Fatal("rebound address dialed")
	}
}
func TestB24RedirectBlockedAndDialPinned(t *testing.T) {
	var requests, dials atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if !strings.HasPrefix(r.Host, "safe.example:") {
			t.Error("Host not preserved")
		}
		if r.URL.Path == "/allowed" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Location", "http://127.0.0.1/metadata")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "https://"))
	tr := server.Client().Transport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // isolated test certificate only
	c := Client(&http.Client{Transport: NewTransport(tr, answer("93.184.216.34"), func(ctx context.Context, n, address string) (net.Conn, error) {
		dials.Add(1)
		if address != net.JoinHostPort("93.184.216.34", port) {
			t.Errorf("dial not pinned: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, n, net.JoinHostPort("127.0.0.1", port))
	})})
	resp, err := c.Get("https://safe.example:" + port + "/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 || requests.Load() != 1 || dials.Load() != 1 {
		t.Fatal("redirect followed")
	}
	accepted, err := c.Get("https://safe.example:" + port + "/allowed")
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Body.Close()
	if accepted.StatusCode != 200 || requests.Load() != 2 || dials.Load() != 2 {
		t.Fatal("public HTTPS callback failed")
	}
}
func TestB24ArbitraryClientCannotBypassPolicy(t *testing.T) {
	c := Client(&http.Client{Transport: http.DefaultTransport})
	if _, err := c.Get("https://127.0.0.1"); err == nil {
		t.Fatal("unguarded client bypassed policy")
	}
}
