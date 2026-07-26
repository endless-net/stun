package deployment_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallReleaseAndAutomaticRollback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the production installer targets Linux")
	}

	root := t.TempDir()
	base := filepath.Join(root, "opt", "endlessnet-stun")
	config := filepath.Join(root, "etc", "stun.env")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("ENDLESSNET_STUN_METRICS_ADDR=127.0.0.1:19090\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	systemctlLog := filepath.Join(root, "systemctl.log")
	curlLog := filepath.Join(root, "curl.log")
	failCurlOnce := filepath.Join(root, "fail-curl-once")
	systemctl := filepath.Join(binDir, "systemctl")
	curl := filepath.Join(binDir, "curl")
	writeExecutable(t, systemctl, `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_SYSTEMCTL_LOG"
`)
	writeExecutable(t, curl, `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_CURL_LOG"
if [ -e "$FAKE_CURL_FAIL_ONCE" ]; then
  remaining=$(cat "$FAKE_CURL_FAIL_ONCE")
  remaining=$((remaining - 1))
  if [ "$remaining" -gt 0 ]; then
    printf '%s\n' "$remaining" > "$FAKE_CURL_FAIL_ONCE"
  else
    rm -f "$FAKE_CURL_FAIL_ONCE"
  fi
  exit 22
fi
`)

	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "install-release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"ENDLESSNET_STUN_TEST_MODE=1",
		"ENDLESSNET_STUN_INSTALL_BASE="+base,
		"ENDLESSNET_STUN_CONFIG_FILE="+config,
		"ENDLESSNET_STUN_SERVICE_NAME=endlessnet-stun.service",
		"ENDLESSNET_STUN_SYSTEMCTL="+systemctl,
		"ENDLESSNET_STUN_CURL="+curl,
		"ENDLESSNET_STUN_TEST_LOCAL_IPS=",
		"ENDLESSNET_STUN_READINESS_ATTEMPTS=1",
		"FAKE_SYSTEMCTL_LOG="+systemctlLog,
		"FAKE_CURL_LOG="+curlLog,
		"FAKE_CURL_FAIL_ONCE="+failCurlOnce,
	)

	install := func(version string, wantSuccess bool) {
		t.Helper()
		artifact := filepath.Join(root, "upload-"+version)
		writeExecutable(t, artifact, "#!/bin/sh\n# "+version+"\n[ \"${1:-}\" = --check-config ]\n")
		content, err := os.ReadFile(artifact)
		if err != nil {
			t.Fatal(err)
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(content))
		cmd := exec.Command("bash", script, version, artifact, checksum)
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		if wantSuccess && err != nil {
			t.Fatalf("install %s failed: %v\n%s", version, err, output)
		}
		if !wantSuccess && err == nil {
			t.Fatalf("install %s unexpectedly succeeded\n%s", version, output)
		}
	}

	if err := os.WriteFile(failCurlOnce, []byte("3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	install("v0.9.0", false)
	assertPathAbsent(t, filepath.Join(base, "current"))
	assertPathAbsent(t, filepath.Join(base, "deployed-version"))

	install("v1.0.0", true)
	assertCurrentVersion(t, base, "v1.0.0")
	install("v1.0.1", true)
	assertCurrentVersion(t, base, "v1.0.1")
	assertFileValue(t, filepath.Join(base, "previous-release"), filepath.Join(base, "releases", "v1.0.0"))
	commandsBeforeBadChecksum := readFile(t, systemctlLog)
	badArtifact := filepath.Join(root, "upload-v1.0.2-bad")
	writeExecutable(t, badArtifact, "#!/bin/sh\n[ \"${1:-}\" = --check-config ]\n")
	badChecksum := strings.Repeat("0", 64)
	badCommand := exec.Command("bash", script, "v1.0.2", badArtifact, badChecksum)
	badCommand.Env = env
	if output, err := badCommand.CombinedOutput(); err == nil {
		t.Fatalf("checksum mismatch unexpectedly succeeded\n%s", output)
	}
	assertCurrentVersion(t, base, "v1.0.1")
	if commandsAfter := readFile(t, systemctlLog); commandsAfter != commandsBeforeBadChecksum {
		t.Fatalf("checksum rejection touched the service\nbefore:\n%s\nafter:\n%s", commandsBeforeBadChecksum, commandsAfter)
	}

	if err := os.WriteFile(failCurlOnce, []byte("3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	install("v1.0.2", false)
	assertCurrentVersion(t, base, "v1.0.1")
	assertFileValue(t, filepath.Join(base, "deployed-version"), "v1.0.1")

	commands := readFile(t, systemctlLog)
	for _, line := range strings.Split(strings.TrimSpace(commands), "\n") {
		if !strings.HasSuffix(line, " endlessnet-stun.service") {
			t.Fatalf("installer touched an unexpected service: %q", line)
		}
	}
	if got := strings.Count(commands, "restart endlessnet-stun.service"); got != 5 {
		t.Fatalf("restart count = %d, want 5 (four deploy attempts plus rollback)\n%s", got, commands)
	}
	if got := strings.Count(commands, "is-active --quiet endlessnet-stun.service"); got != 5 {
		t.Fatalf("readiness service checks = %d, want 5\n%s", got, commands)
	}
	if got := strings.Count(commands, "stop endlessnet-stun.service"); got != 1 {
		t.Fatalf("failed first-install stops = %d, want 1\n%s", got, commands)
	}
	curls := readFile(t, curlLog)
	if got := strings.Count(curls, "http://127.0.0.1:19090/readyz"); got != 5 {
		t.Fatalf("readiness HTTP checks = %d, want 5\n%s", got, curls)
	}
}

func TestInstallReleaseRetriesReadiness(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the production installer targets Linux")
	}

	root := t.TempDir()
	base := filepath.Join(root, "opt", "endlessnet-stun")
	config := filepath.Join(root, "stun.env")
	if err := os.WriteFile(config, []byte("ENDLESSNET_STUN_METRICS_ADDR=127.0.0.1:19090\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	systemctl := filepath.Join(binDir, "systemctl")
	curl := filepath.Join(binDir, "curl")
	curlCount := filepath.Join(root, "curl-count")
	writeExecutable(t, systemctl, "#!/bin/sh\nexit 0\n")
	writeExecutable(t, curl, `#!/bin/sh
count=$(cat "$FAKE_CURL_COUNT" 2>/dev/null || printf 0)
count=$((count + 1))
printf '%s\n' "$count" > "$FAKE_CURL_COUNT"
[ "$count" -ge 4 ]
`)
	artifact := filepath.Join(root, "upload")
	writeExecutable(t, artifact, "#!/bin/sh\n[ \"${1:-}\" = --check-config ]\n")
	content, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256(content))
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "install-release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, "v1.0.9", artifact, checksum)
	cmd.Env = append(os.Environ(),
		"ENDLESSNET_STUN_TEST_MODE=1",
		"ENDLESSNET_STUN_INSTALL_BASE="+base,
		"ENDLESSNET_STUN_CONFIG_FILE="+config,
		"ENDLESSNET_STUN_SERVICE_NAME=endlessnet-stun.service",
		"ENDLESSNET_STUN_SYSTEMCTL="+systemctl,
		"ENDLESSNET_STUN_CURL="+curl,
		"ENDLESSNET_STUN_TEST_LOCAL_IPS=",
		"ENDLESSNET_STUN_READINESS_ATTEMPTS=2",
		"FAKE_CURL_COUNT="+curlCount,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("installer did not tolerate transient readiness: %v\n%s", err, output)
	}
	assertCurrentVersion(t, base, "v1.0.9")
	assertFileValue(t, curlCount, "4")
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%s exists after failed first installation or returned error %v", path, err)
	}
}

func TestConfigureMetricsBindUpdatesOnlyTheOwnedSetting(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the production configurator targets Linux")
	}

	root := t.TempDir()
	config := filepath.Join(root, "stun.env")
	original := strings.Join([]string{
		"# managed STUN configuration",
		"ENDLESSNET_STUN_ADDRS=0.0.0.0:3478",
		"ENDLESSNET_STUN_METRICS_ADDR=45.152.87.196:9090",
		"ENDLESSNET_STUN_RATE_LIMIT_BURST=40",
		"ENDLESSNET_STUN_METRICS_ADDR=duplicate.example:9091",
		"",
	}, "\n")
	if err := os.WriteFile(config, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "configure-metrics-bind.sh"))
	if err != nil {
		t.Fatal(err)
	}
	run := func(address string) error {
		cmd := exec.Command("bash", script, address)
		cmd.Env = append(os.Environ(),
			"ENDLESSNET_STUN_TEST_MODE=1",
			"ENDLESSNET_STUN_CONFIG_FILE="+config,
		)
		return cmd.Run()
	}
	if err := run("127.0.0.1:9090"); err != nil {
		t.Fatal(err)
	}
	updated := readFile(t, config)
	if got := strings.Count(updated, "ENDLESSNET_STUN_METRICS_ADDR="); got != 1 {
		t.Fatalf("metrics setting count = %d, want 1\n%s", got, updated)
	}
	for _, line := range []string{
		"# managed STUN configuration",
		"ENDLESSNET_STUN_ADDRS=0.0.0.0:3478",
		"ENDLESSNET_STUN_METRICS_ADDR=127.0.0.1:9090",
		"ENDLESSNET_STUN_RATE_LIMIT_BURST=40",
	} {
		if !strings.Contains(updated, line) {
			t.Fatalf("updated config lost %q\n%s", line, updated)
		}
	}
	beforeInvalid := updated
	if err := run("0.0.0.0:9090"); err == nil {
		t.Fatal("public metrics bind address unexpectedly succeeded")
	}
	if afterPublic := readFile(t, config); afterPublic != beforeInvalid {
		t.Fatalf("public update changed config\nbefore:\n%s\nafter:\n%s", beforeInvalid, afterPublic)
	}
	if err := run("bad/address"); err == nil {
		t.Fatal("invalid metrics bind address unexpectedly succeeded")
	}
	if afterInvalid := readFile(t, config); afterInvalid != beforeInvalid {
		t.Fatalf("invalid update changed config\nbefore:\n%s\nafter:\n%s", beforeInvalid, afterInvalid)
	}
}

func TestDeployProdUpdatesTargetsSequentiallyAndStopsAfterFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the production deployment orchestrator targets Linux")
	}

	root := t.TempDir()
	releaseDir := filepath.Join(root, "release")
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	artifactName := "endlessnet-stun_v1.0.2_linux_amd64"
	artifact := filepath.Join(releaseDir, artifactName)
	writeExecutable(t, artifact, "#!/bin/sh\nexit 0\n")
	content, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256(content))
	if err := os.WriteFile(filepath.Join(releaseDir, "checksums.txt"), []byte(checksum+"  "+artifactName+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(root, "deploy.log")
	writeExecutable(t, filepath.Join(fakeBin, "gh"), `#!/bin/sh
pattern=
dir=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --pattern) pattern=$2; shift 2 ;;
    --dir) dir=$2; shift 2 ;;
    *) shift ;;
  esac
done
cp "$FAKE_RELEASE_DIR/$pattern" "$dir/$pattern"
`)
	writeExecutable(t, filepath.Join(fakeBin, "ssh"), `#!/bin/sh
host=$1
shift
printf 'ssh %s %s\n' "$host" "$*" >> "$FAKE_DEPLOY_LOG"
case "$*" in
  "uname -m") echo x86_64 ;;
  *"mktemp /tmp/endlessnet-stun.XXXXXX"*) echo "/tmp/endlessnet-stun.${host##*@}" ;;
  *) cat >/dev/null ;;
esac
`)
	writeExecutable(t, filepath.Join(fakeBin, "scp"), `#!/bin/sh
printf 'scp %s\n' "$*" >> "$FAKE_DEPLOY_LOG"
`)
	smoke := filepath.Join(fakeBin, "smoke")
	writeExecutable(t, smoke, `#!/bin/sh
printf 'smoke %s\n' "$*" >> "$FAKE_DEPLOY_LOG"
case "$*" in
  *fail.example*) exit 1 ;;
esac
`)

	deployScript, err := filepath.Abs(filepath.Join("..", "..", "scripts", "deploy-prod.sh"))
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_RELEASE_DIR="+releaseDir,
		"FAKE_DEPLOY_LOG="+logPath,
		"DEPLOY_USER=deploy",
		"GITHUB_REPOSITORY=unng-lab/endlessnet-stun",
		"SMOKE_BINARY="+smoke,
	)
	runDeploy := func(endpoints string) error {
		t.Helper()
		cmd := exec.Command("bash", deployScript,
			"--version", "v1.0.2",
			"--targets", "node-a,node-b",
			"--stun-endpoints", endpoints,
			"--metrics-bind-address", "127.0.0.1:9090",
			"--strategy", "rolling",
		)
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("deploy output:\n%s", output)
		}
		return err
	}

	if err := runDeploy("stun-a.example:3478,stun-b.example:3478"); err != nil {
		t.Fatal(err)
	}
	assertOrdered(t, readFile(t, logPath), []string{
		"ssh deploy@node-a sudo env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin bash -s -- '127.0.0.1:9090'",
		"ssh deploy@node-a uname -m",
		"ssh deploy@node-a mktemp /tmp/endlessnet-stun.XXXXXX",
		"deploy@node-a:/tmp/endlessnet-stun.node-a",
		"ssh deploy@node-a sudo env -i",
		"smoke --stun-addr stun-a.example:3478",
		"ssh deploy@node-b sudo env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin bash -s -- '127.0.0.1:9090'",
		"ssh deploy@node-b uname -m",
		"ssh deploy@node-b mktemp /tmp/endlessnet-stun.XXXXXX",
		"deploy@node-b:/tmp/endlessnet-stun.node-b",
		"ssh deploy@node-b sudo env -i",
		"smoke --stun-addr stun-b.example:3478",
	})

	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runDeploy("fail.example:3478,stun-b.example:3478"); err == nil {
		t.Fatal("deployment unexpectedly continued after a failed public smoke test")
	}
	failureLog := readFile(t, logPath)
	if strings.Contains(failureLog, "node-b") {
		t.Fatalf("second target was touched after first-target failure:\n%s", failureLog)
	}
	if got := strings.Count(failureLog, "smoke --stun-addr fail.example:3478"); got != 2 {
		t.Fatalf("failed deployment and rollback smoke count = %d, want 2\n%s", got, failureLog)
	}
}

func assertOrdered(t *testing.T, text string, markers []string) {
	t.Helper()
	position := 0
	for _, marker := range markers {
		index := strings.Index(text[position:], marker)
		if index < 0 {
			t.Fatalf("marker %q was not found in order after byte %d:\n%s", marker, position, text)
		}
		position += index + len(marker)
	}
}

func assertCurrentVersion(t *testing.T, base, version string) {
	t.Helper()
	target, err := filepath.EvalSymlinks(filepath.Join(base, "current"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "releases", version)
	if target != want {
		t.Fatalf("current = %q, want %q", target, want)
	}
	assertFileValue(t, filepath.Join(base, "deployed-version"), version)
}

func assertFileValue(t *testing.T, path, want string) {
	t.Helper()
	if got := strings.TrimSpace(readFile(t, path)); got != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
