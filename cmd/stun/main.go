package main

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/endless-net/stun/internal/server"
)

func main() { os.Exit(run()) }

func run() int {
	c, err := server.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "config_error")
		return 2
	}
	if c.Version {
		fmt.Println("stun v0 small-profile")
		return 0
	}
	if c.Check {
		fmt.Println("config_ok")
		return 0
	}
	debug.SetMemoryLimit(192 << 20)
	a := server.App(c, os.Stderr)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = a.Start(ctx)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "startup_error")
		return 1
	}
	signal := <-a.Wait()
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Stop(ctx); err != nil {
		return 1
	}
	return signal.ExitCode
}
