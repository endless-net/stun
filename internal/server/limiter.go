package server

import (
	"net/netip"
	"time"
)

const sourceIdleTTL = 300 * time.Second

type bucket struct {
	tokens   float64
	lastSeen time.Time
}
type limiter struct {
	sources map[netip.Addr]bucket
	sweep   time.Time
}

func newLimiter() *limiter { return &limiter{sources: make(map[netip.Addr]bucket)} }

// Only the owning listener worker accesses a limiter.
func (l *limiter) allow(ip netip.Addr, now time.Time) bool {
	ip = ip.Unmap()
	if l.sweep.IsZero() || now.Sub(l.sweep) >= sourceIdleTTL {
		for k, v := range l.sources {
			if now.Sub(v.lastSeen) >= sourceIdleTTL {
				delete(l.sources, k)
			}
		}
		l.sweep = now
	}
	b, ok := l.sources[ip]
	if !ok {
		if len(l.sources) >= 65536 {
			return false
		}
		b = bucket{tokens: 40, lastSeen: now}
	}
	b.tokens = min(40, b.tokens+max(0, now.Sub(b.lastSeen).Seconds())*20)
	b.lastSeen = now
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	l.sources[ip] = b
	return allowed
}
