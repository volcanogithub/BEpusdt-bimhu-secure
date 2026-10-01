package access

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestB25TokenDigestBindingExpirationAndRevocation(t *testing.T) {
	now := time.Unix(1000, 0)
	s := New(func() time.Time { return now })
	token, err := s.Issue("session")
	if err != nil {
		t.Fatal(err)
	}
	if s.digest != sha256.Sum256([]byte(token)) || len(token) != 43 {
		t.Fatal("token must contain 256 bits of entropy; stored value must be its digest")
	}
	for _, tc := range []struct {
		token, session string
		want           bool
	}{
		{token, "session", true}, {"", "session", false}, {token + "x", "session", false},
		{token[:42], "session", false}, {token, "other", false}, {token, "", false},
	} {
		if s.Verify(tc.token, tc.session) != tc.want {
			t.Fatal("unexpected token verification")
		}
	}
	now = now.Add(Lifetime - time.Nanosecond)
	if !s.Verify(token, "session") {
		t.Fatal("premature expiration")
	}
	now = now.Add(time.Nanosecond)
	if s.Verify(token, "session") {
		t.Fatal("must expire at exact boundary")
	}
	newToken, err := s.Issue("session")
	if err != nil {
		t.Fatal(err)
	}
	if token == newToken || s.Verify(token, "session") || !s.Verify(newToken, "session") {
		t.Fatal("replacement login must revoke old token")
	}
	s.Revoke()
	if s.Verify(newToken, "session") || s.session != "" || s.digest != ([32]byte{}) {
		t.Fatal("revoke failed")
	}
}

func TestB25LoginWindowCooldownAndSuccess(t *testing.T) {
	now := time.Unix(1000, 0)
	s := New(func() time.Time { return now })
	for i := 0; i < UserAttempts; i++ {
		if !s.Attempt("admin", fmt.Sprint(i)) {
			t.Fatal("attempt below user limit denied")
		}
	}
	if s.Attempt("admin", "new-ip") {
		t.Fatal("user limit bypassed with IP rotation")
	}
	now = now.Add(Cooldown - time.Nanosecond)
	if s.Attempt("admin", "new-ip") {
		t.Fatal("cooldown ended early")
	}
	now = now.Add(time.Nanosecond)
	if !s.Attempt("admin", "new-ip") {
		t.Fatal("exact cooldown boundary must recover")
	}
	s.Success("admin")
	if _, ok := s.buckets[key("user", "admin")]; ok {
		t.Fatal("success must clear user attempts")
	}
	if s.buckets[key("ip", "new-ip")].count != 1 {
		t.Fatal("success must not clear IP work budget")
	}
	now = now.Add(Window)
	if !s.Attempt("admin", "new-ip") || s.buckets[key("ip", "new-ip")].count != 1 {
		t.Fatal("window did not reset")
	}
}

func TestB25IPLimitConcurrencyAndBoundedMemory(t *testing.T) {
	now := time.Unix(1000, 0)
	s := New(func() time.Time { return now })
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if s.Attempt(fmt.Sprint(i), "shared-ip") {
				admitted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if admitted.Load() != IPAttempts {
		t.Fatalf("admitted %d, want %d", admitted.Load(), IPAttempts)
	}
	now = now.Add(Cooldown)
	if !s.Attempt("next", "shared-ip") {
		t.Fatal("IP cooldown did not recover")
	}
	s = New(func() time.Time { return now })
	for i := 0; i < MaxBuckets/2; i++ {
		if !s.Attempt(fmt.Sprint(i), fmt.Sprint(i)) {
			t.Fatal("capacity prematurely exhausted")
		}
	}
	if s.Attempt("overflow", "overflow") || len(s.buckets) != MaxBuckets {
		t.Fatal("capacity must fail closed without growing")
	}
	now = now.Add(Window)
	if !s.Attempt("overflow", "overflow") || len(s.buckets) != 2 {
		t.Fatal("capacity did not recover after window")
	}
}

func TestB25CooldownStartsAtThresholdAndDeniedAttemptsDoNotExtendIt(t *testing.T) {
	now := time.Unix(1000, 0)
	s := New(func() time.Time { return now })
	for i := 0; i < UserAttempts-1; i++ {
		if !s.Attempt("admin", "ip") {
			t.Fatal("premature limit")
		}
	}
	now = now.Add(Window - time.Nanosecond)
	if !s.Attempt("admin", "ip") {
		t.Fatal("last window attempt denied")
	}
	now = now.Add(time.Nanosecond)
	if s.Attempt("admin", "ip") {
		t.Fatal("window boundary must not bypass threshold cooldown")
	}
	now = now.Add(Cooldown - time.Nanosecond)
	if !s.Attempt("admin", "ip") {
		t.Fatal("denied attempts must not extend cooldown")
	}
}
