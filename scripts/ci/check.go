package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

func goEnvironment(extra ...string) []string {
	return append([]string{"GOWORK=off", "GOFLAGS=-mod=readonly"}, extra...)
}

func (r runner) check(ctx context.Context, profile profile, lintConfig string, goOnly bool) error {
	if lintConfig != "" {
		path, err := filepath.Abs(lintConfig)
		if err != nil {
			return err
		}
		lintConfig = path
	}
	for _, module := range profile.Modules {
		where := filepath.Join(r.root, module.Path)
		fmt.Println("CHECK", module.Kind, module.Path)
		config := lintConfig
		if config == "" {
			config = filepath.Join(where, ".golangci-lint.yaml")
		}
		if err := r.checkGo(ctx, where, config, goOnly); err != nil {
			return err
		}
	}
	return nil
}

func (r runner) checkGo(ctx context.Context, where, config string, goOnly bool) error {
	env := goEnvironment()
	output, err := r.run(ctx, where, env, "go", "mod", "edit", "-json")
	if err != nil {
		return err
	}
	var metadata struct {
		Replace []struct{ Old struct{ Path string } }
	}
	if err := json.Unmarshal(output, &metadata); err != nil {
		return err
	}
	for _, entry := range metadata.Replace {
		if strings.HasPrefix(entry.Old.Path, "github.com/endless-net/") {
			return errors.New("first-party replace is forbidden")
		}
	}
	if !goOnly {
		output, err := r.run(ctx, where, env, "goimports", "-l", ".")
		if err != nil {
			return err
		}
		if len(output) != 0 {
			return fmt.Errorf("go source formatting differs:\n%s", output)
		}
		if _, err := r.run(ctx, where, env, "go", "vet", "./..."); err != nil {
			return err
		}
	}
	packages := []string{"./..."}
	if goOnly {
		listed, err := r.run(ctx, where, env, "go", "list", "./...")
		if err != nil {
			return err
		}
		packages = slices.DeleteFunc(strings.Fields(string(listed)), func(pkg string) bool {
			return pkg == "github.com/endless-net/stun/api"
		})
		if len(packages) == 0 {
			return errors.New("no runtime Go packages found")
		}
	}
	for _, args := range [][]string{
		{"golangci-lint", "run", "--config", config, "--timeout", "1m"},
		{"go", "test", "-short"},
		{"govulncheck"},
	} {
		output, err := r.run(ctx, where, env, append(args, packages...)...)
		if err != nil {
			return err
		}
		fmt.Print(string(output))
	}
	return nil
}
