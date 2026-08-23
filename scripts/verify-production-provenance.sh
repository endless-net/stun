#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 --version vX.Y.Z [--workflow-ref refs/heads/main]" >&2
}

version=
workflow_ref=
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) version=${2:-}; shift 2 ;;
    --workflow-ref) workflow_ref=${2:-}; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]] || {
  echo "version must be an exact v-prefixed semantic version" >&2
  exit 2
}
if [[ -n "$workflow_ref" && "$workflow_ref" != refs/heads/main ]]; then
  echo "release provenance verification must run from refs/heads/main" >&2
  exit 1
fi

repository=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}
gh_bin=${GH_BIN:-gh}

git fetch --no-tags origin '+refs/heads/main:refs/remotes/origin/main'
git fetch --force origin "refs/tags/${version}:refs/tags/${version}"
release_commit=$(git rev-parse "${version}^{commit}")
if ! git merge-base --is-ancestor "$release_commit" refs/remotes/origin/main; then
  echo "$version does not reference a commit in origin/main" >&2
  exit 1
fi

merged_pr_count=$(
  "$gh_bin" api \
    -H 'Accept: application/vnd.github+json' \
    -H 'X-GitHub-Api-Version: 2022-11-28' \
    "repos/${repository}/commits/${release_commit}/pulls" \
    --jq '[.[] | select(.merged_at != null and .base.ref == "main")] | length'
)
[[ "$merged_pr_count" =~ ^[0-9]+$ ]] || {
  echo "GitHub returned an invalid merged PR count" >&2
  exit 1
}
if (( merged_pr_count == 0 )); then
  root_commit=$(git rev-list --max-parents=0 refs/remotes/origin/main)
  commit_count=$(git rev-list --count refs/remotes/origin/main)
  if [[ "$version" == v1.0.10 && "$commit_count" == 1 && "$release_commit" == "$root_commit" ]]; then
    echo "verified $version as the single-commit public baseline"
    exit 0
  fi
  echo "$version commit $release_commit was not delivered through a merged PR into main" >&2
  exit 1
fi

echo "verified $version commit $release_commit from a merged PR into main"
