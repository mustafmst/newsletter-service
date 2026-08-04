package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	limit int
	now   func() time.Time
	mu    sync.Mutex
	hits  map[string]bucket
}

type bucket struct {
	minute int64
	count  int
}

func New(limit int, now func() time.Time) *Limiter {
	return &Limiter{
		limit: limit,
		now:   now,
		hits:  map[string]bucket{},
	}
}

func (l *Limiter) Allow(key string) bool {
	if l.limit <= 0 {
		return true
	}
	minute := l.now().Unix() / 60
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(minute)
	current := l.hits[key]
	if current.minute != minute {
		current = bucket{minute: minute}
	}
	if current.count >= l.limit {
		l.hits[key] = current
		return false
	}
	current.count++
	l.hits[key] = current
	return true
}

func (l *Limiter) pruneLocked(currentMinute int64) {
	for key, hit := range l.hits {
		if hit.minute < currentMinute {
			delete(l.hits, key)
		}
	}
}
