package access

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestB25DirectPeerIPCanonicalizationAndInvalidAddresses(t *testing.T) {
	for _, tc := range []struct{ address, want string }{
		{"198.51.100.1:1234", "198.51.100.1"},
		{"[2001:db8::1]:1234", "2001:db8::1"},
		{"[::ffff:198.51.100.1]:1234", "198.51.100.1"},
		{"", "unknown"}, {"invalid:1234", "unknown"}, {"198.51.100.1", "unknown"},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/api/auth/login", nil)
		c.Request.RemoteAddr = tc.address
		c.Request.Header.Set("X-Forwarded-For", "203.0.113.1")
		c.Request.Header.Set("X-Real-IP", "203.0.113.2")
		if got := ClientIP(c); got != tc.want {
			t.Fatalf("peer %q: got %q, want %q", tc.address, got, tc.want)
		}
	}
}
