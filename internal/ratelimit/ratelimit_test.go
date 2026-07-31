package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterAllowsOnlyConfiguredRequestsPerMinute(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	limiter := New(2, func() time.Time { return now })

	if !limiter.Allow("203.0.113.10") {
		t.Fatal("first request should be allowed")
	}
	if !limiter.Allow("203.0.113.10") {
		t.Fatal("second request should be allowed")
	}
	if limiter.Allow("203.0.113.10") {
		t.Fatal("third request in same minute should be rejected")
	}

	now = now.Add(time.Minute)
	if !limiter.Allow("203.0.113.10") {
		t.Fatal("request in next minute should be allowed")
	}
}

func TestLimiterKeepsKeysIndependent(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	limiter := New(1, func() time.Time { return now })

	if !limiter.Allow("203.0.113.10") {
		t.Fatal("first key should be allowed")
	}
	if !limiter.Allow("203.0.113.11") {
		t.Fatal("second key should be allowed independently")
	}
}
