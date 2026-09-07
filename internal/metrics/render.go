package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func (r *Registry) Render() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	listeners := make(map[string]bool, len(r.listeners))
	for listener, active := range r.listeners {
		listeners[listener] = active
	}
	counters := make(map[counterKey]uint64, len(r.counters))
	for key, value := range r.counters {
		counters[key] = value
	}
	durations := make(map[durationKey]durationValue, len(r.durations))
	for key, value := range r.durations {
		durations[key] = value
	}
	buildInfo := r.buildInfo
	r.mu.RUnlock()

	var b strings.Builder
	writeHelpType(&b, "stun_requests_total", "Accepted STUN datagrams received.", "counter")
	writeCounterFamily(&b, counters, "stun_requests_total")
	writeHelpType(&b, "stun_responses_total", "STUN Binding success responses sent.", "counter")
	writeCounterFamily(&b, counters, "stun_responses_total")
	writeHelpType(&b, "stun_invalid_requests_total", "Malformed or unsupported STUN requests rejected.", "counter")
	writeCounterFamily(&b, counters, "stun_invalid_requests_total")
	writeHelpType(&b, "stun_rate_limited_total", "STUN requests rejected by source-IP rate limiting.", "counter")
	writeCounterFamily(&b, counters, "stun_rate_limited_total")
	writeHelpType(&b, "stun_errors_total", "STUN listener and response errors.", "counter")
	writeCounterFamily(&b, counters, "stun_errors_total")

	writeHelpType(&b, "stun_active_listeners", "Whether a configured STUN listener is active.", "gauge")
	listenerNames := sortedListenerNames(listeners)
	for _, listener := range listenerNames {
		value := 0
		if listeners[listener] {
			value = 1
		}
		fmt.Fprintf(&b, "stun_active_listeners{listener=%s} %d\n", quote(listener), value)
	}

	writeHelpType(&b, "stun_request_duration_seconds", "Time spent handling STUN requests.", "summary")
	durationKeys := make([]durationKey, 0, len(durations))
	for key := range durations {
		durationKeys = append(durationKeys, key)
	}
	sort.Slice(durationKeys, func(i, j int) bool {
		if durationKeys[i].listener == durationKeys[j].listener {
			return durationKeys[i].result < durationKeys[j].result
		}
		return durationKeys[i].listener < durationKeys[j].listener
	})
	for _, key := range durationKeys {
		labels := fmt.Sprintf("listener=%s,result=%s", quote(key.listener), quote(key.result))
		value := durations[key]
		fmt.Fprintf(&b, "stun_request_duration_seconds_sum{%s} %.9f\n", labels, value.sum.Seconds())
		fmt.Fprintf(&b, "stun_request_duration_seconds_count{%s} %d\n", labels, value.count)
	}

	writeHelpType(&b, "stun_build_info", "Build metadata for the running STUN service.", "gauge")
	fmt.Fprintf(
		&b,
		"stun_build_info{build_date=%s,commit=%s,executable_digest=%s,version=%s} 1\n",
		quote(buildInfo.BuildDate),
		quote(buildInfo.Commit),
		quote(buildInfo.ExecutableDigest),
		quote(buildInfo.Version),
	)
	return b.String()
}

func writeCounterFamily(b *strings.Builder, counters map[counterKey]uint64, name string) {
	keys := make([]counterKey, 0)
	for key := range counters {
		if key.name == name {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return counterSortKey(keys[i]) < counterSortKey(keys[j])
	})
	for _, key := range keys {
		labels := []string{"listener=" + quote(key.listener)}
		if key.addressFamily != "" {
			labels = append(labels, "address_family="+quote(key.addressFamily))
		}
		if key.result != "" {
			labels = append(labels, "result="+quote(key.result))
		}
		fmt.Fprintf(b, "%s{%s} %d\n", name, strings.Join(labels, ","), counters[key])
	}
}

func counterSortKey(key counterKey) string {
	return key.listener + "\x00" + key.addressFamily + "\x00" + key.result
}

func sortedListenerNames(listeners map[string]bool) []string {
	names := make([]string, 0, len(listeners))
	for name := range listeners {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func writeHelpType(b *strings.Builder, name, help, metricType string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
}

func quote(value string) string {
	return strconv.Quote(value)
}
