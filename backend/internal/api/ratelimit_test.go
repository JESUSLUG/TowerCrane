package api

import (
	"testing"
	"time"
)

func TestAttemptLimiter(t *testing.T) {
	l := &attemptLimiter{maxFails: 3, window: time.Minute, m: map[string]*attempts{}}
	for i := 0; i < 3; i++ {
		if !l.allow("A@x.io") {
			t.Fatalf("attempt %d refused too early", i)
		}
		l.fail("a@x.io ")
	}
	if l.allow("a@x.io") {
		t.Fatal("4th attempt should be refused")
	}
	if !l.allow("b@x.io") {
		t.Fatal("other emails are not affected")
	}
	l.m["a@x.io"].first = time.Now().Add(-2 * time.Minute)
	if !l.allow("a@x.io") {
		t.Fatal("the window should expire")
	}
	l.fail("a@x.io")
	l.reset("a@x.io")
	if _, ok := l.m["a@x.io"]; ok {
		t.Fatal("reset should forget the email")
	}
}
