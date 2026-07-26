package ratelimit

import (
	"net"
	"testing"
	"time"
)

func TestLimiterEnforcesBurstAndRefillsPerSourceIP(t *testing.T) {
	l := NewWithBounds(2, 2, time.Minute, 10)
	now := time.Unix(100, 0)
	a := &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1000}
	if !l.Allow(a, now) || !l.Allow(&net.UDPAddr{IP: a.IP, Port: 2000}, now) {
		t.Fatal("initial burst was not allowed")
	}
	if l.Allow(a, now) {
		t.Fatal("request beyond burst was allowed")
	}
	if !l.Allow(a, now.Add(500*time.Millisecond)) {
		t.Fatal("one replenished token was not allowed")
	}
}

func TestLimiterBoundsTrackedSourcesAndCleansIdleEntries(t *testing.T) {
	l := NewWithBounds(1, 1, time.Second, 1)
	now := time.Unix(100, 0)
	if !l.Allow(&net.UDPAddr{IP: net.ParseIP("192.0.2.1")}, now) {
		t.Fatal("first source rejected")
	}
	if l.Allow(&net.UDPAddr{IP: net.ParseIP("192.0.2.2")}, now) {
		t.Fatal("source above bound allowed")
	}
	if !l.Allow(&net.UDPAddr{IP: net.ParseIP("192.0.2.2")}, now.Add(2*time.Second)) {
		t.Fatal("new source rejected after idle cleanup")
	}
}
