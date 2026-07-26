package deployment_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVerifyProductionProvenance(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the production provenance gate runs on Linux")
	}

	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	work := filepath.Join(root, "work")
	runGit(t, root, "init", "--bare", origin)
	runGit(t, root, "init", "--initial-branch=main", work)
	runGit(t, work, "config", "user.name", "STUN provenance test")
	runGit(t, work, "config", "user.email", "stun-provenance@example.test")
	if err := os.WriteFile(filepath.Join(work, "release.txt"), []byte("v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "release.txt")
	runGit(t, work, "commit", "-m", "release through PR")
	runGit(t, work, "remote", "add", "origin", origin)
	runGit(t, work, "push", "--set-upstream", "origin", "main")
	runGit(t, work, "tag", "-a", "v1.2.3", "-m", "v1.2.3")
	runGit(t, work, "push", "origin", "v1.2.3")

	fakeGH := filepath.Join(root, "gh")
	writeExecutable(t, fakeGH, `#!/bin/sh
printf '%s\n' "${FAKE_MERGED_PR_COUNT:-0}"
`)
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "verify-production-provenance.sh"))
	if err != nil {
		t.Fatal(err)
	}

	verify := func(version, workflowRef, mergedPRCount string, wantSuccess bool) {
		t.Helper()
		args := []string{script, "--version", version}
		if workflowRef != "" {
			args = append(args, "--workflow-ref", workflowRef)
		}
		cmd := exec.Command("bash", args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(),
			"GH_BIN="+fakeGH,
			"GITHUB_REPOSITORY=unng-lab/endlessnet-stun",
			"FAKE_MERGED_PR_COUNT="+mergedPRCount,
		)
		output, runErr := cmd.CombinedOutput()
		if wantSuccess && runErr != nil {
			t.Fatalf("verify %s failed: %v\n%s", version, runErr, output)
		}
		if !wantSuccess && runErr == nil {
			t.Fatalf("verify %s unexpectedly succeeded:\n%s", version, output)
		}
	}

	verify("v1.2.3", "refs/heads/main", "1", true)
	verify("v1.2.3", "refs/heads/main", "0", false)
	verify("v1.2.3", "refs/heads/feature", "1", false)

	runGit(t, work, "switch", "-c", "feature")
	if err := os.WriteFile(filepath.Join(work, "release.txt"), []byte("v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "release.txt")
	runGit(t, work, "commit", "-m", "unmerged release")
	runGit(t, work, "tag", "-a", "v1.2.4", "-m", "v1.2.4")
	runGit(t, work, "push", "origin", "feature", "v1.2.4")
	verify("v1.2.4", "", "1", false)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
