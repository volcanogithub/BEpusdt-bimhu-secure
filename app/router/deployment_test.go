package router

import (
	"github.com/gin-gonic/gin"
	"github.com/v03413/bepusdt/app/log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestB26SecurityHeadersNormalFailureAndUnknownRoutes(t *testing.T) {
	f := newAccessFixture(t)
	for _, path := range []string{"/", "/api/auth/info", "/not-found"} {
		w := f.request("GET", path, "", "", "198.51.100.1", nil, false)
		for name, value := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "SAMEORIGIN", "Referrer-Policy": "no-referrer", "Content-Security-Policy": deploymentCSP} {
			if w.Header().Get(name) != value {
				t.Fatalf("%s missing %s", path, name)
			}
		}
	}
	for _, directive := range []string{"script-src 'self' 'unsafe-inline'", "style-src 'self' 'unsafe-inline'", "img-src 'self' data: https:", "object-src 'none'", "frame-ancestors 'self'"} {
		if !strings.Contains(deploymentCSP, directive) {
			t.Fatal("CSP compatibility or confinement missing")
		}
	}
}

func TestB26RequestLogsExcludeSecretPathsQueriesAndHeaders(t *testing.T) {
	f := newAccessFixture(t)
	path := "/b26-sensitive-path?token=b26-sensitive-query"
	f.request("GET", path, "password=b26-sensitive-body", "b26-sensitive-header", "198.51.100.1", nil, false)
	dir := log.GetPath()
	data, err := os.ReadFile(filepath.Join(dir, "bepusdt.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "b26-sensitive") {
		t.Fatal("request log leaked raw request data")
	}
	if !strings.Contains(string(data), "[unmatched]") {
		t.Fatal("request not logged with safe route template")
	}
}

func TestB26PanicRecoveryDoesNotDumpRequestsOrPanicValues(t *testing.T) {
	f := newAccessFixture(t)
	f.e.GET("/test-panic", func(c *gin.Context) { panic("b26-sensitive-panic") })
	w := f.request("GET", "/test-panic?arbitrary=b26-sensitive-query", "", "b26-sensitive-header", "198.51.100.1", nil, false)
	if w.Code != 500 || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("panic response not protected")
	}
	data, err := os.ReadFile(filepath.Join(log.GetPath(), "bepusdt.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "b26-sensitive") || !strings.Contains(string(data), "http panic recovered") {
		t.Fatal("panic log leaked request or omitted event")
	}
}
