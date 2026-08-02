#!/usr/bin/env bash

set -euo pipefail

repository_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/endlessnet-stun-contracts.XXXXXX")
cleanup() {
  rm -rf -- "$work"
}
trap cleanup EXIT HUP INT TERM

commit_sha=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
publisher_commit=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
producer_run_id=101
commit_ci_run_id=102
publisher_ci_run_id=103
rekor_index=104
artifact_name="endlessnet-stun-$commit_sha"
archive_name="$artifact_name.tar.gz"
stage="$work/stage"

mkdir -p \
  "$stage/bin/linux-amd64" \
  "$stage/bin/linux-arm64" \
  "$stage/systemd"
for architecture in amd64 arm64; do
  printf '#!/usr/bin/env sh\nexit 0\n' >"$stage/bin/linux-$architecture/endlessnet-stun"
  printf '#!/usr/bin/env sh\nexit 0\n' >"$stage/bin/linux-$architecture/endlessnet-stun-smoke"
  chmod 0755 \
    "$stage/bin/linux-$architecture/endlessnet-stun" \
    "$stage/bin/linux-$architecture/endlessnet-stun-smoke"
done
printf '[Service]\nExecStart=/opt/endlessnet-stun/current/endlessnet-stun\n' \
  >"$stage/systemd/endlessnet-stun.service"
printf 'license fixture\n' >"$stage/LICENSE"
printf 'notice fixture\n' >"$stage/NOTICE"
printf 'third-party fixture\n' >"$stage/THIRD_PARTY_NOTICES"
tar -C "$stage" -czf "$work/$archive_name" .
archive_sha256=$(sha256sum "$work/$archive_name" | awk '{print $1}')
artifact_digest="sha256:$archive_sha256"
printf '%s  %s\n' "$archive_sha256" "$archive_name" >"$work/$archive_name.sha256"

jq -n \
  --arg repository endless-net/stun \
  --arg workflow .github/workflows/publish-production.yml \
  --arg run_id "$producer_run_id" \
  --arg commit_ci_run_id "$commit_ci_run_id" \
  --arg publisher_commit "$publisher_commit" \
  --arg publisher_ci_run_id "$publisher_ci_run_id" \
  --arg commit_sha "$commit_sha" \
  --arg artifact_name "$artifact_name" \
  --arg artifact_digest "$artifact_digest" \
  --arg statement "$archive_name.intoto.json" \
  --arg bundle "$archive_name.provenance.sigstore.json" \
  --arg rekor_index "$rekor_index" '
    {
      schema_version: 1,
      service: "stun",
      producer_repository: $repository,
      producer_workflow: $workflow,
      producer_run_id: ($run_id | tonumber),
      commit_ci_run_id: ($commit_ci_run_id | tonumber),
      publisher_workflow_commit: $publisher_commit,
      publisher_ci_run_id: ($publisher_ci_run_id | tonumber),
      commit_sha: $commit_sha,
      artifact_name: $artifact_name,
      artifact_digest: $artifact_digest,
      architectures: ["amd64", "arm64"],
      created_at: "2026-08-02T00:00:00Z",
      provenance: {
        statement: $statement,
        sigstore_bundle: $bundle,
        certificate_identity: "https://github.com/endless-net/stun/.github/workflows/publish-production.yml@refs/heads/main",
        certificate_issuer: "https://token.actions.githubusercontent.com",
        rekor_log_index: ($rekor_index | tonumber)
      }
    }
  ' >"$work/$artifact_name.manifest.json"

jq -n \
  --arg archive_name "$archive_name" \
  --arg archive_sha256 "$archive_sha256" \
  --arg run_id "$producer_run_id" \
  --arg commit_ci_run_id "$commit_ci_run_id" \
  --arg publisher_commit "$publisher_commit" \
  --arg publisher_ci_run_id "$publisher_ci_run_id" \
  --arg commit_sha "$commit_sha" '
    {
      _type: "https://in-toto.io/Statement/v1",
      subject: [{name: $archive_name, digest: {sha256: $archive_sha256}}],
      predicateType: "https://slsa.dev/provenance/v1",
      predicate: {
        buildDefinition: {
          buildType: "https://endlessnet.ru/build-types/stun-linux-systemd-archive/v1",
          externalParameters: {commit_sha: $commit_sha},
          internalParameters: {
            producer_repository: "endless-net/stun",
            producer_workflow: ".github/workflows/publish-production.yml",
            producer_run_id: ($run_id | tonumber),
            commit_ci_run_id: ($commit_ci_run_id | tonumber),
            publisher_workflow_commit: $publisher_commit,
            publisher_ci_run_id: ($publisher_ci_run_id | tonumber)
          },
          resolvedDependencies: [{
            uri: "git+https://github.com/endless-net/stun",
            digest: {gitCommit: $commit_sha}
          }]
        },
        runDetails: {
          builder: {id: "https://github.com/endless-net/stun/.github/workflows/publish-production.yml@refs/heads/main"},
          metadata: {invocationId: ("https://github.com/endless-net/stun/actions/runs/" + $run_id)}
        }
      }
    }
  ' >"$work/$archive_name.intoto.json"
jq -n --arg rekor_index "$rekor_index" \
  '{verificationMaterial: {tlogEntries: [{logIndex: ($rekor_index | tonumber)}]}}' \
  >"$work/$archive_name.provenance.sigstore.json"

verify_artifact() {
  ARTIFACT_DIR="$work" \
  COMMIT_SHA="$commit_sha" \
  COMMIT_CI_RUN_ID="$commit_ci_run_id" \
  PRODUCER_REPOSITORY=endless-net/stun \
  PRODUCER_RUN_ID="$producer_run_id" \
  PUBLISHER_COMMIT_SHA="$publisher_commit" \
  PUBLISHER_CI_RUN_ID="$publisher_ci_run_id" \
  ARTIFACT_DIGEST="$artifact_digest" \
  SIGSTORE_REKOR_INDEX="$rekor_index" \
    bash "$repository_root/scripts/verify-production-artifact.sh"
}

verify_artifact

publication="$work/$artifact_name.manifest.json"
cp "$publication" "$publication.valid"
jq '.target = "producer-must-not-select-target"' "$publication.valid" >"$publication"
if verify_artifact 2>/dev/null; then
  echo "manifest with producer-selected target unexpectedly passed" >&2
  exit 1
fi
mv "$publication.valid" "$publication"

publisher="$repository_root/.github/workflows/publish-production.yml"
# The literal GitHub expression is the contract under test.
# shellcheck disable=SC2016
grep -Fqx 'run-name: ${{ inputs.commit_sha }}' "$publisher"
if grep -Eq 'DEPLOY_|SSH|known.host|target|inventory|secrets:|secrets\.' "$publisher"; then
  echo "production publisher contains deployment authority" >&2
  exit 1
fi
if grep -Eq 'uses:[[:space:]]+endless-net/infrastructure/' "$publisher"; then
  echo "Infrastructure caller must wait for an exact STUN entrypoint" >&2
  exit 1
fi
