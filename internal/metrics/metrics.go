package metrics

import (
	"strings"
	"sync"
	"time"
)

type Registry struct {
	buildInfo BuildInfo

	mu        sync.RWMutex
	listeners map[string]bool
	counters  map[counterKey]uint64
	durations map[durationKey]durationValue
}

// BuildInfo is safe, immutable revision evidence for the running executable.
// It intentionally contains no host, endpoint, credential, or configuration
// values.
type BuildInfo struct {
	Version          string `json:"version"`
	Commit           string `json:"commit"`
	BuildDate        string `json:"build_date"`
	ExecutableDigest string `json:"executable_digest"`
}

type counterKey struct {
	name          string
	listener      string
	addressFamily string
	result        string
}

type durationKey struct {
	listener string
	result   string
}

type durationValue struct {
	sum   time.Duration
	count uint64
}

func New(info BuildInfo) *Registry {
	if strings.TrimSpace(info.Version) == "" {
		info.Version = "dev"
	}
	if strings.TrimSpace(info.Commit) == "" {
		info.Commit = "unknown"
	}
	if strings.TrimSpace(info.BuildDate) == "" {
		info.BuildDate = "unknown"
	}
	if strings.TrimSpace(info.ExecutableDigest) == "" {
		info.ExecutableDigest = "unknown"
	}
	return &Registry{
		buildInfo: info,
		listeners: make(map[string]bool),
		counters:  make(map[counterKey]uint64),
		durations: make(map[durationKey]durationValue),
	}
}

func (r *Registry) BuildInfo() BuildInfo {
	if r == nil {
		return BuildInfo{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.buildInfo
}

func (r *Registry) SetListener(listener string, active bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.listeners[listener] = active
	r.mu.Unlock()
}

func (r *Registry) Ready() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, active := range r.listeners {
		if !active {
			return false
		}
	}
	return len(r.listeners) > 0
}

func (r *Registry) RecordRequest(listener, family string) {
	r.increment(counterKey{name: "stun_requests_total", listener: listener, addressFamily: family})
}

func (r *Registry) RecordResponse(listener, family string) {
	r.increment(counterKey{name: "stun_responses_total", listener: listener, addressFamily: family})
}

func (r *Registry) RecordInvalid(listener, family string) {
	r.increment(counterKey{name: "stun_invalid_requests_total", listener: listener, addressFamily: family})
}

func (r *Registry) RecordRateLimited(listener, family string) {
	r.increment(counterKey{name: "stun_rate_limited_total", listener: listener, addressFamily: family})
}

func (r *Registry) RecordError(listener, result string) {
	r.increment(counterKey{name: "stun_errors_total", listener: listener, result: result})
}

func (r *Registry) ObserveDuration(listener, result string, duration time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	key := durationKey{listener: listener, result: result}
	value := r.durations[key]
	value.sum += duration
	value.count++
	r.durations[key] = value
	r.mu.Unlock()
}

func (r *Registry) increment(key counterKey) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.counters[key]++
	r.mu.Unlock()
}
