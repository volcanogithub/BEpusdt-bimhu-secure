package access

import (
	"net"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/sessions"
	"github.com/v03413/bepusdt/app/conf"
)

// ClientIP uses the direct peer only for administrator login protections.
// Do not change Gin's global proxy behavior, which also feeds payment fingerprints.
func ClientIP(c *gin.Context) string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return "unknown"
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "unknown"
	}
	return ip.String()
}

// TLS is determined only from the actual connection, not spoofable proxy headers.
func CookieOptions(c *gin.Context) sessions.Options {
	return sessions.Options{Path: "/", MaxAge: int(Lifetime.Seconds()), HttpOnly: true,
		Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode}
}

func InvalidateSession(c *gin.Context) error {
	s := sessions.Default(c)
	s.Clear()
	o := CookieOptions(c)
	o.MaxAge = -1
	s.Options(o)
	return s.Save() // memstore deletes server-side values, rejecting old cookies.
}

// RotateSession deletes the old server-side session before assigning a fresh ID.
func RotateSession(c *gin.Context) error {
	if err := InvalidateSession(c); err != nil {
		return err
	}
	s := sessions.Default(c)
	// gin-contrib's session exposes the underlying gorilla session through Session.
	s.(interface{ Session() *gorilla.Session }).Session().ID = ""
	s.Options(CookieOptions(c))
	s.Set(conf.AdminSecureK, true)
	s.Set(conf.AdminSessionAtK, Default.Now().UnixNano())
	return s.Save()
}

func ValidGate(c *gin.Context) bool {
	s := sessions.Default(c)
	secure, _ := s.Get(conf.AdminSecureK).(bool)
	at, ok := s.Get(conf.AdminSessionAtK).(int64)
	now := Default.Now().UnixNano()
	return secure && ok && at <= now && now-at < int64(Lifetime)
}
