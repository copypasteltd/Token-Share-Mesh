package controlhttp

import (
	"sync"
	"time"
)

type bucket struct {
	begins time.Time
	count  int
}
type limiter struct {
	mu       sync.Mutex
	entries  map[string]bucket
	capacity int
}

func (l *limiter) take(key string, limit int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.entries[key]
	if !ok && len(l.entries) >= l.capacity {
		for key, b := range l.entries {
			if now.Sub(b.begins) >= time.Minute {
				delete(l.entries, key)
			}
		}
		if len(l.entries) >= l.capacity {
			return false
		}
	}
	if !ok || now.Sub(b.begins) >= time.Minute {
		b = bucket{begins: now}
	}
	if b.count >= limit {
		return false
	}
	b.count++
	l.entries[key] = b
	return true
}
