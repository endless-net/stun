#!/usr/bin/env bash
set -euo pipefail
commit=${1:?exact commit SHA is required}
[[ "$commit" =~ ^[0-9a-f]{40}$ ]]
repository=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}
gh_bin=${GH_BIN:-gh}
runs=$("$gh_bin" api --method GET "repos/$repository/actions/workflows/ci.yml/runs" \
  -f head_sha="$commit" -f branch=main -f status=success -f per_page=100)
run_id=$(jq -r --arg sha "$commit" '[.workflow_runs[] | select(.head_sha == $sha and .head_branch == "main" and .event == "push" and .path == ".github/workflows/ci.yml" and .status == "completed" and .conclusion == "success")][0].id // empty' <<<"$runs")
[[ "$run_id" =~ ^[0-9]+$ ]] || { echo 'No successful main CI for exact commit' >&2; exit 1; }
jobs=$("$gh_bin" api --paginate --slurp "repos/$repository/actions/runs/$run_id/jobs?filter=latest&per_page=100")
for required in verify stun-e2e-linux stun-e2e-windows stun-e2e-container; do
  jq -e --arg name "$required" '[.[].jobs[] | select(.name == $name and .conclusion == "success")] | length == 1' <<<"$jobs" >/dev/null || {
    echo "Missing successful exact-commit product E2E: $required" >&2; exit 1;
  }
done
echo "Verified checks and product E2E for $commit in CI run $run_id" >&2
printf '%s\n' "$run_id"
