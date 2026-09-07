//go:build e2e

// Product E2E intentionally does not import the product wire codec.
package e2e_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var serverBinary, smokeBinary, evidence string

func TestMain(m *testing.M) { os.Exit(runTests(m)) }

func runTests(m *testing.M) int {
	work, err := os.MkdirTemp("", "stun-product-e2e-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(work)
	evidence = os.Getenv("STUN_E2E_EVIDENCE")
	if evidence == "" {
		evidence = filepath.Join(work, "evidence")
	}
	if err = os.MkdirAll(evidence, 0700); err != nil {
		panic(err)
	}
	serverBinary = filepath.Join(work, "stun")
	smokeBinary = filepath.Join(work, "smoke")
	if runtime.GOOS == "windows" {
		serverBinary += ".exe"
		smokeBinary += ".exe"
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	metadata := map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version()}
	for _, item := range []struct{ path, command string }{{serverBinary, "endlessnet-stun"}, {smokeBinary, "endlessnet-stun-smoke"}} {
		cmd := exec.Command("go", "build", "-o", item.path, "./cmd/"+item.command)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "build: %v\n%s", err, out)
			return 1
		}
		data, err := os.ReadFile(item.path)
		if err != nil {
			panic(err)
		}
		metadata[item.command+"_sha256"] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	if out, err := cmd.Output(); err == nil {
		metadata["commit"] = strings.TrimSpace(string(out))
	}
	if image := os.Getenv("STUN_E2E_IMAGE"); image != "" {
		out, err := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", image).Output()
		if err != nil {
			panic(err)
		}
		metadata["image_id"] = strings.TrimSpace(string(out))
	}
	data, _ := json.MarshalIndent(metadata, "", "  ")
	if err := os.WriteFile(filepath.Join(evidence, "identity.json"), data, 0600); err != nil {
		panic(err)
	}
	return m.Run()
}
