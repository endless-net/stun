package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/unng-lab/endlessnet-stun/internal/stun"
)

type result struct {
	STUNAddress   string `json:"stun_address"`
	MappedAddress string `json:"mapped_address"`
	Latency       string `json:"latency"`
}

func main() {
	stunAddr := flag.String("stun-addr", "", "public STUN host:port")
	timeout := flag.Duration("timeout", 5*time.Second, "overall probe timeout")
	flag.Parse()
	if strings.TrimSpace(*stunAddr) == "" || *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "stun-addr and a positive timeout are required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	started := time.Now()
	mapped, err := stun.Query(ctx, *stunAddr)
	if err != nil {
		fail("STUN Binding probe", err)
	}
	latency := time.Since(started)
	if err := json.NewEncoder(os.Stdout).Encode(result{
		STUNAddress:   *stunAddr,
		MappedAddress: mapped.String(),
		Latency:       latency.String(),
	}); err != nil {
		fail("encode result", err)
	}
}

func fail(step string, err error) {
	fmt.Fprintf(os.Stderr, "%s failed: %v\n", step, err)
	os.Exit(1)
}
