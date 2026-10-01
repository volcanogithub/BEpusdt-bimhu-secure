package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/v03413/bepusdt/app/access"
	"github.com/v03413/bepusdt/app/conf"
	"github.com/v03413/bepusdt/app/log"
	"github.com/v03413/bepusdt/app/model"
	"golang.org/x/crypto/bcrypt"
)

type accessFixture struct {
	e   *gin.Engine
	now time.Time
}

func newAccessFixture(t *testing.T) *accessFixture {
	t.Helper()
	f := &accessFixture{now: time.Unix(1800000000, 0)}
	previous := access.Default
	access.Default = access.New(func() time.Time { return f.now })
	if err := log.Init(filepath.Join(t.TempDir(), "logs")); err != nil {
		t.Fatal(err)
	}
	if err := model.Init(filepath.Join(t.TempDir(), "access.db"), ""); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.SetSecretValues(map[model.ConfKey]string{
		model.AdminUsername: "test-admin", model.AdminPassword: string(hash), model.AdminSecure: "/b25-entry",
	}); err != nil {
		t.Fatal(err)
	}
	authRoute = make(map[string]bool)
	secureRoute = make(map[string]struct{})
	f.e = Handler()
	t.Cleanup(func() { model.Close(); log.Close(); access.Default = previous })
	return f
}

func (f *accessFixture) request(method, path, body, token, ip string, cookie *http.Cookie, tls bool) *httptest.ResponseRecorder {
	scheme := "http"
	if tls {
		scheme = "https"
	}
	r := httptest.NewRequest(method, scheme+"://example.test"+path, strings.NewReader(body))
	r.RemoteAddr = ip + ":12345"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", token)
	r.Header.Set("X-Forwarded-For", "192.0.2.99")
	r.Header.Set("X-Real-IP", "192.0.2.98")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.e.ServeHTTP(w, r)
	return w
}

func lastCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	var result *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			result = c
		}
	}
	if result == nil {
		t.Fatal("expected session cookie")
	}
	return result
}

func (f *accessFixture) gate(t *testing.T, tls bool) *http.Cookie {
	t.Helper()
	w := f.request("GET", "/b25-entry", "", "", "198.51.100.1", nil, tls)
	if w.Code != 302 {
		t.Fatalf("gate HTTP %d", w.Code)
	}
	return lastCookie(t, w)
}

func (f *accessFixture) login(t *testing.T, cookie *http.Cookie) (string, *http.Cookie) {
	t.Helper()
	w := f.request("POST", "/api/auth/login", `{"username":"test-admin","password":"correct-password"}`, "", "198.51.100.1", cookie, false)
	if w.Code != 200 {
		t.Fatalf("login HTTP %d", w.Code)
	}
	var result struct {
		Code int
		Data struct{ Token string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Code != 200 || result.Data.Token == "" {
		t.Fatal("login did not return token")
	}
	return result.Data.Token, lastCookie(t, w)
}

func TestB25NormalLoginAndSessionRotation(t *testing.T) {
	f := newAccessFixture(t)
	gate := f.gate(t, false)
	token, cookie := f.login(t, gate)
	if cookie.Value == gate.Value {
		t.Fatal("login must rotate session ID")
	}
	if model.GetK(model.AdminLoginIP) != "198.51.100.1" {
		t.Fatal("forwarded IP headers must not be trusted")
	}
	w := f.request("GET", "/api/auth/info", "", token, "198.51.100.1", cookie, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"code":200`) {
		t.Fatal("authenticated access denied")
	}
	if f.request("GET", "/api/auth/info", "", token, "198.51.100.1", gate, false).Code != 403 {
		t.Fatal("old gate cookie was not invalidated")
	}
}

func TestB25InvalidCredentialsAreUniformAndBounded(t *testing.T) {
	f := newAccessFixture(t)
	gate := f.gate(t, false)
	var expected string
	for _, body := range []string{
		`{"username":"test-admin","password":"wrong"}`,
		`{"username":"unknown","password":"correct-password"}`,
		`{"username":"unknown","password":"wrong"}`, `{}`, `{`,
		`{"username":"oversized","password":"` + strings.Repeat("x", 4096) + `"}`,
	} {
		w := f.request("POST", "/api/auth/login", body, "", "198.51.100.1", gate, false)
		if w.Code != 401 {
			t.Fatalf("invalid login HTTP %d", w.Code)
		}
		if expected == "" {
			expected = w.Body.String()
		}
		if w.Body.String() != expected || !strings.Contains(expected, "invalid credentials") {
			t.Fatal("credential errors must be indistinguishable")
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("failed login must not issue a session")
		}
	}
}

func TestB25BruteForceCooldownAndRecovery(t *testing.T) {
	f := newAccessFixture(t)
	gate := f.gate(t, false)
	for i := 0; i < access.UserAttempts; i++ {
		w := f.request("POST", "/api/auth/login", `{"username":"test-admin","password":"wrong"}`, "", fmt.Sprintf("198.51.100.%d", i+1), gate, false)
		if w.Code != 401 {
			t.Fatalf("failure %d HTTP %d", i+1, w.Code)
		}
	}
	correct := `{"username":"test-admin","password":"correct-password"}`
	for _, advance := range []time.Duration{0, access.Cooldown - time.Nanosecond} {
		f.now = f.now.Add(advance)
		w := f.request("POST", "/api/auth/login", correct, "", "203.0.113.1", gate, false)
		if w.Code != 429 || w.Header().Get("Retry-After") != "300" || !strings.Contains(w.Body.String(), "invalid credentials") {
			t.Fatal("user cooldown not enforced")
		}
	}
	f.now = f.now.Add(time.Nanosecond)
	f.login(t, gate)
}

func TestB25IPLimitWithUsernameAndForwardedHeaderRotation(t *testing.T) {
	f := newAccessFixture(t)
	gate := f.gate(t, false)
	for i := 0; i < access.IPAttempts; i++ {
		w := f.request("POST", "/api/auth/login", fmt.Sprintf(`{"username":"unknown-%d","password":"wrong"}`, i), "", "198.51.100.1", gate, false)
		if w.Code != 401 {
			t.Fatalf("IP attempt %d HTTP %d", i, w.Code)
		}
	}
	w := f.request("POST", "/api/auth/login", `{"username":"another","password":"wrong"}`, "", "198.51.100.1", gate, false)
	if w.Code != 429 {
		t.Fatal("IP limit bypassed by rotating usernames")
	}
	f.now = f.now.Add(access.Cooldown)
	f.login(t, gate)
}

func TestB25AllAdministratorRoutesDenyUnauthenticatedRequests(t *testing.T) {
	f := newAccessFixture(t)
	gate := f.gate(t, false)
	token, cookie := f.login(t, gate)
	otherGate := f.gate(t, false)
	f.e.GET("/api/conf/unregistered", func(c *gin.Context) { c.Status(200) })
	checked := 0
	for _, route := range f.e.Routes() {
		if !strings.HasPrefix(route.Path, "/api/") || strings.HasPrefix(route.Path, "/api/v1/") || route.Path == "/api/auth/login" {
			continue
		}
		checked++
		for _, pair := range []struct {
			cookie *http.Cookie
			token  string
		}{
			{nil, ""}, {cookie, ""}, {nil, token}, {cookie, "wrong-token"}, {otherGate, token},
		} {
			w := f.request(route.Method, route.Path, `{}`, pair.token, "198.51.100.1", pair.cookie, false)
			if w.Code != 403 {
				t.Fatalf("%s %s: unauthenticated HTTP %d", route.Method, route.Path, w.Code)
			}
		}
	}
	if checked < 30 {
		t.Fatalf("only checked %d admin routes", checked)
	}
	if f.request("POST", "/api/auth/login", `{}`, "", "198.51.100.1", nil, false).Code != 403 {
		t.Fatal("login requires entrance session")
	}
}

func TestB25LogoutAndPasswordChangeInvalidateOldSessions(t *testing.T) {
	for _, operation := range []string{"logout", "set_password"} {
		t.Run(operation, func(t *testing.T) {
			f := newAccessFixture(t)
			token, cookie := f.login(t, f.gate(t, false))
			body := `{}`
			if operation == "set_password" {
				body = `{"password":"correct-password","new_password":"new-password","confirm_password":"new-password"}`
			}
			w := f.request("POST", "/api/auth/"+operation, body, token, "198.51.100.1", cookie, false)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"code":200`) {
				t.Fatal("operation failed")
			}
			if lastCookie(t, w).MaxAge != -1 {
				t.Fatal("cookie was not deleted")
			}
			if access.Default.Verify(token, "") {
				t.Fatal("token survived invalidation")
			}
			if f.request("GET", "/api/auth/info", "", token, "198.51.100.1", cookie, false).Code != 403 {
				t.Fatal("old cookie/token replay accepted")
			}
			if f.request("POST", "/api/auth/login", `{}`, "", "198.51.100.1", cookie, false).Code != 403 {
				t.Fatal("old session still permits login")
			}
			gate := f.gate(t, false)
			if operation == "logout" {
				f.login(t, gate)
			} else {
				w = f.request("POST", "/api/auth/login", `{"username":"test-admin","password":"correct-password"}`, "", "198.51.100.1", gate, false)
				if w.Code != 401 {
					t.Fatal("old password accepted")
				}
				w = f.request("POST", "/api/auth/login", `{"username":"test-admin","password":"new-password"}`, "", "198.51.100.1", gate, false)
				if w.Code != 200 || !strings.Contains(w.Body.String(), `"token"`) {
					t.Fatal("new password rejected")
				}
			}
		})
	}
}

func TestB25CookieFlagsAndAbsoluteSessionExpiration(t *testing.T) {
	f := newAccessFixture(t)
	if w := f.request("POST", "/b25-entry", "", "", "198.51.100.1", nil, false); w.Code != 404 || len(w.Result().Cookies()) != 0 {
		t.Fatal("secret entrance must activate sessions only for GET")
	}
	for _, tls := range []bool{false, true} {
		cookie := f.gate(t, tls)
		if !cookie.HttpOnly || cookie.Secure != tls || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge != 86400 {
			t.Fatal("unsafe cookie policy")
		}
	}
	gate := f.gate(t, false)
	token, cookie := f.login(t, gate)
	f.now = f.now.Add(access.Lifetime - time.Nanosecond)
	if f.request("GET", "/api/auth/info", "", token, "198.51.100.1", cookie, false).Code != 200 {
		t.Fatal("premature expiry")
	}
	f.now = f.now.Add(time.Nanosecond)
	if f.request("GET", "/api/auth/info", "", token, "198.51.100.1", cookie, false).Code != 403 {
		t.Fatal("expired session accepted")
	}
	if f.request("POST", "/api/auth/login", `{}`, "", "198.51.100.1", cookie, false).Code != 403 {
		t.Fatal("expired gate accepted")
	}
	f.login(t, f.gate(t, false))
}

func TestB25LegacyAndFutureSessionTimestampsAreDenied(t *testing.T) {
	f := newAccessFixture(t)
	f.e.GET("/test-session", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set(conf.AdminSecureK, true)
		if c.Query("future") == "1" {
			s.Set(conf.AdminSessionAtK, f.now.Add(time.Second).UnixNano())
		}
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		c.Status(200)
	})
	for _, path := range []string{"/test-session", "/test-session?future=1"} {
		w := f.request("GET", path, "", "", "198.51.100.1", nil, false)
		cookie := lastCookie(t, w)
		if f.request("POST", "/api/auth/login", `{"username":"test-admin","password":"correct-password"}`, "", "198.51.100.1", cookie, false).Code != 403 {
			t.Fatal("legacy or future entrance session accepted")
		}
	}
}

func TestB25ReplacementLoginRevokesPreviousAuthentication(t *testing.T) {
	f := newAccessFixture(t)
	oldToken, oldCookie := f.login(t, f.gate(t, false))
	newToken, newCookie := f.login(t, f.gate(t, false))
	if oldToken == newToken {
		t.Fatal("tokens reused")
	}
	if f.request("GET", "/api/auth/info", "", oldToken, "198.51.100.1", oldCookie, false).Code != 403 {
		t.Fatal("replacement login left previous authentication active")
	}
	if f.request("GET", "/api/auth/info", "", newToken, "198.51.100.1", newCookie, false).Code != 200 {
		t.Fatal("replacement login cannot access API")
	}
}

func TestB25RejectedPasswordChangesPreserveLogin(t *testing.T) {
	f := newAccessFixture(t)
	token, cookie := f.login(t, f.gate(t, false))
	initialHash := model.GetK(model.AdminPassword)
	for _, body := range []string{
		`{}`,
		`{"password":"wrong","new_password":"new-password","confirm_password":"new-password"}`,
		`{"password":"correct-password","new_password":"new-password","confirm_password":"different"}`,
		`{"password":"correct-password","new_password":"12345","confirm_password":"12345"}`,
		`{"password":"correct-password","new_password":"` + strings.Repeat("x", 73) + `","confirm_password":"` + strings.Repeat("x", 73) + `"}`,
	} {
		w := f.request("POST", "/api/auth/set_password", body, token, "198.51.100.1", cookie, false)
		if !strings.Contains(w.Body.String(), `"code":400`) {
			t.Fatal("invalid password change accepted")
		}
		if model.GetK(model.AdminPassword) != initialHash {
			t.Fatal("rejected change modified stored password")
		}
		if f.request("GET", "/api/auth/info", "", token, "198.51.100.1", cookie, false).Code != 200 {
			t.Fatal("rejected password change revoked valid login")
		}
	}
}
