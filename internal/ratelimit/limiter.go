package ratelimit

import (
	"net"
	"strings"
	"sync"
	"time"
)

const (
	DefaultIdleTTL    = 5 * time.Minute
	DefaultMaxSources = 65536
)

type Limiter struct {
	ratePerSecond float64
	burst         float64
	idleTTL       time.Duration
	maxSources    int

	mu          sync.Mutex
	buckets     map[string]*bucket
	lastCleanup time.Time
}

type bucket struct {
	tokens   float64
	last     time.Time
	lastSeen time.Time
}

func New(ratePerSecond, burst int) *Limiter {
	return NewWithBounds(ratePerSecond, burst, DefaultIdleTTL, DefaultMaxSources)
}

func NewWithBounds(ratePerSecond, burst int, idleTTL time.Duration, maxSources int) *Limiter {
	if ratePerSecond <= 0 || burst <= 0 || idleTTL <= 0 || maxSources <= 0 {
		panic("ratelimit: all limits must be positive")
	}
	return &Limiter{
		ratePerSecond: float64(ratePerSecond),
		burst:         float64(burst),
		idleTTL:       idleTTL,
		maxSources:    maxSources,
		buckets:       make(map[string]*bucket),
	}
}

func (l *Limiter) Allow(addr net.Addr, now time.Time) bool {
	if l == nil {
		return false
	}
	key := sourceKey(addr)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= l.idleTTL {
		l.cleanup(now)
	}
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= l.maxSources {
			return false
		}
		b = &bucket{tokens: l.burst, last: now, lastSeen: now}
		l.buckets[key] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * l.ratePerSecond
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = now
	}
	b.lastSeen = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *Limiter) cleanup(now time.Time) {
	for key, b := range l.buckets {
		if now.Sub(b.lastSeen) >= l.idleTTL {
			delete(l.buckets, key)
		}
	}
	l.lastCleanup = now
}

func sourceKey(addr net.Addr) string {
	if udp, ok := addr.(*net.UDPAddr); ok && udp.IP != nil {
		return udp.IP.String()
	}
	if addr == nil {
		return "unknown"
	}
	return strings.TrimSpace(addr.String())
}
