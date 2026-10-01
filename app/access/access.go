// Package access owns ephemeral administrator authentication state only.
package access

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"sync"
	"time"
)

const (
	Lifetime     = 24 * time.Hour
	Window       = 5 * time.Minute
	Cooldown     = 5 * time.Minute
	UserAttempts = 5
	IPAttempts   = 20
	MaxBuckets   = 4096
)

type bucket struct {
	count int
	until time.Time
}

// State keeps one active administrator login, matching the existing model.
// It retains only a digest of the bearer token, never the returned plaintext.
type State struct {
	mu      sync.Mutex
	now     func() time.Time
	buckets map[[32]byte]bucket
	digest  [32]byte
	session string
	expires time.Time
}

func New(now func() time.Time) *State {
	return &State{now: now, buckets: make(map[[32]byte]bucket)}
}

var Default = New(time.Now)

func key(kind, value string) [32]byte { return sha256.Sum256([]byte(kind + "\x00" + value)) }

// Attempt reserves work before bcrypt, including concurrent and malformed attempts.
// Successful login clears the username bucket but retains the IP work budget.
// Saturation fails closed until bounded buckets expire; no unbounded key growth.
func (s *State) Attempt(username, ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, b := range s.buckets {
		if !now.Before(b.until) {
			delete(s.buckets, k)
		}
	}
	keys := [2][32]byte{key("user", username), key("ip", ip)}
	limits := [2]int{UserAttempts, IPAttempts}
	missing := 0
	for i, k := range keys {
		b, exists := s.buckets[k]
		if exists && b.count >= limits[i] {
			return false
		}
		if !exists {
			missing++
		}
	}
	if len(s.buckets)+missing > MaxBuckets {
		return false
	}
	for i, k := range keys {
		b := s.buckets[k]
		if b.count == 0 {
			b.until = now.Add(Window)
		}
		b.count++
		if b.count == limits[i] {
			b.until = now.Add(Cooldown)
		}
		s.buckets[k] = b
	}
	return true
}

func (s *State) Success(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.buckets, key("user", username))
}

func (s *State) Issue(session string) (string, error) {
	var material [32]byte
	if _, err := rand.Read(material[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(material[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	s.digest = sha256.Sum256([]byte(token))
	s.session = session
	s.expires = s.now().Add(Lifetime)
	return token, nil
}

func (s *State) Verify(token, session string) bool {
	digest := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	return subtle.ConstantTimeCompare(s.digest[:], digest[:]) == 1 &&
		token != "" && session != "" && session == s.session && s.now().Before(s.expires)
}

func (s *State) Revoke() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.digest = [32]byte{}
	s.session = ""
	s.expires = time.Time{}
}

func (s *State) Now() time.Time { return s.now() }
