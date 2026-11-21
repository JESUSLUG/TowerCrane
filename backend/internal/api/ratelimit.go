package api

import (
	"strings"
	"sync"
	"time"
)

// attemptLimiter slows down password guessing: after maxFails failed
// sign-ins for one email within window, that email is refused until the
// window passes. Keyed by email, not IP, so it can't be dodged by changing
// address (or spoofing X-Forwarded-For).
type attemptLimiter struct {
	mu       sync.Mutex
	maxFails int
	window   time.Duration
	m        map[string]*attempts
}

type attempts struct {
	n     int
	first time.Time
}

var loginAttempts = &attemptLimiter{maxFails: 10, window: 15 * time.Minute, m: map[string]*attempts{}}

func (l *attemptLimiter) key(k string) string { return strings.ToLower(strings.TrimSpace(k)) }

func (l *attemptLimiter) allow(k string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.m[l.key(k)]
	if a == nil || time.Since(a.first) > l.window {
		return true
	}
	return a.n < l.maxFails
}

func (l *attemptLimiter) fail(k string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	k = l.key(k)
	a := l.m[k]
	if a == nil || time.Since(a.first) > l.window {
		// Forget expired entries now and then so the map can't grow forever.
		if len(l.m) > 10000 {
			for key, v := range l.m {
				if time.Since(v.first) > l.window {
					delete(l.m, key)
				}
			}
		}
		l.m[k] = &attempts{n: 1, first: time.Now()}
		return
	}
	a.n++
}

func (l *attemptLimiter) reset(k string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, l.key(k))
}
