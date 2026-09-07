#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/stun-evidence-tests.XXXXXX")
trap 'rm -rf -- "$work"' EXIT
export GITHUB_REPOSITORY=endless-net/stun
export TEST_COMMIT=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export GH_BIN="$work/gh"
cat >"$GH_BIN" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == *'/jobs?'* ]]; then
  jq -n --arg mode "$TEST_MODE" '
    ["verify", "stun-e2e-linux", "stun-e2e-windows", "stun-e2e-container"] |
    map({name: ., conclusion: "success"}) |
    if $mode == "missing" then .[1:]
    elif $mode == "duplicate" then . + [.[0]]
    elif $mode == "failed" then .[0].conclusion = "failure"
    elif $mode == "skipped" then .[1].conclusion = "skipped"
    else . end | [{jobs: .}]'
else
  jq -n --arg mode "$TEST_MODE" --arg sha "$TEST_COMMIT" '
    {id: 123, head_sha: $sha, head_branch: "main", event: "push",
     path: ".github/workflows/ci.yml", status: "completed", conclusion: "success"} |
    if $mode == "wrong-commit" then .head_sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    elif $mode == "pr" then .event = "pull_request"
    elif $mode == "dispatch" then .event = "workflow_dispatch"
    elif $mode == "tag" then .head_branch = "v1.0.13"
    elif $mode == "wrong-workflow" then .path = ".github/workflows/other.yml"
    elif $mode == "incomplete" then .status = "in_progress"
    elif $mode == "failed-run" then .conclusion = "failure"
    else . end | {workflow_runs: [.]}'
fi
SH
chmod +x "$GH_BIN"
export TEST_MODE=success
test "$(bash "$root/scripts/verify-e2e-evidence.sh" "$TEST_COMMIT")" = 123
for TEST_MODE in wrong-commit pr dispatch tag wrong-workflow incomplete failed-run missing duplicate failed skipped; do
  export TEST_MODE
  if bash "$root/scripts/verify-e2e-evidence.sh" "$TEST_COMMIT" >"$work/output" 2>&1; then
    echo "unsafe evidence accepted: $TEST_MODE" >&2
    exit 1
  fi
done
echo 'Exact-commit evidence gate tests passed'
