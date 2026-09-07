#!/usr/bin/env bash
set -euo pipefail
release_id=$(git rev-parse HEAD)
test "$release_id" = "$PUBLISH_COMMIT_SHA"
artifact_name="endlessnet-stun-$release_id"
stage="$RUNNER_TEMP/$artifact_name"
install -d \
  "$stage/bin/linux-amd64" \
  "$stage/bin/linux-arm64" \
  "$stage/systemd"
source_date_epoch=$(git show -s --format=%ct HEAD)
build_date=$(date -u -d "@$source_date_epoch" +%Y-%m-%dT%H:%M:%SZ)
version="commit-${release_id:0:12}"
ldflags="-s -w -X main.version=$version -X main.commit=$release_id -X main.buildDate=$build_date"
for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
    go build -trimpath -buildvcs=true -ldflags="$ldflags" \
    -o "$stage/bin/linux-$arch/endlessnet-stun" \
    ./cmd/endlessnet-stun
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
    go build -trimpath -buildvcs=true -ldflags="-s -w" \
    -o "$stage/bin/linux-$arch/endlessnet-stun-smoke" \
    ./cmd/endlessnet-stun-smoke
done
install -m 0644 deploy/systemd/endlessnet-stun.service "$stage/systemd/"
install -m 0644 LICENSE NOTICE THIRD_PARTY_NOTICES "$stage/"

archive_name="$artifact_name.tar.gz"
archive="$RUNNER_TEMP/$archive_name"
checksum="$archive.sha256"
manifest="$RUNNER_TEMP/$artifact_name.manifest.json"
provenance="$RUNNER_TEMP/$archive_name.intoto.json"
provenance_bundle="$RUNNER_TEMP/$archive_name.provenance.sigstore.json"
tar --sort=name --owner=0 --group=0 --numeric-owner \
  --mtime="@$source_date_epoch" -C "$stage" -czf "$archive" .
archive_sha256=$(sha256sum "$archive" | awk '{print $1}')
printf '%s  %s\n' "$archive_sha256" "$archive_name" >"$checksum"
(
  cd "$RUNNER_TEMP"
  sha256sum -c "$(basename "$checksum")"
)

jq -n \
  --arg schema_version 1 \
  --arg service stun \
  --arg producer_repository "$GITHUB_REPOSITORY" \
  --arg producer_workflow .github/workflows/ci.yml \
  --arg producer_run_id "$GITHUB_RUN_ID" \
  --arg commit_ci_run_id "$COMMIT_CI_RUN_ID" \
  --arg publisher_workflow_commit "$GITHUB_SHA" \
  --arg publisher_ci_run_id "$PUBLISHER_CI_RUN_ID" \
  --arg commit_sha "$release_id" \
  --arg artifact_name "$artifact_name" \
  --arg artifact_digest "sha256:$archive_sha256" \
  --arg created_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '{
    schema_version: ($schema_version | tonumber),
    service: $service,
    producer_repository: $producer_repository,
    producer_workflow: $producer_workflow,
    producer_run_id: ($producer_run_id | tonumber),
    commit_ci_run_id: ($commit_ci_run_id | tonumber),
    publisher_workflow_commit: $publisher_workflow_commit,
    publisher_ci_run_id: ($publisher_ci_run_id | tonumber),
    commit_sha: $commit_sha,
    artifact_name: $artifact_name,
    artifact_digest: $artifact_digest,
    architectures: ["amd64", "arm64"],
    created_at: $created_at
  }' >"$manifest"

jq -n \
  --arg archive_name "$archive_name" \
  --arg archive_sha256 "$archive_sha256" \
  --arg producer_repository "$GITHUB_REPOSITORY" \
  --arg producer_workflow .github/workflows/ci.yml \
  --arg producer_run_id "$GITHUB_RUN_ID" \
  --arg commit_ci_run_id "$COMMIT_CI_RUN_ID" \
  --arg publisher_workflow_commit "$GITHUB_SHA" \
  --arg publisher_ci_run_id "$PUBLISHER_CI_RUN_ID" \
  --arg commit_sha "$release_id" \
  --arg invocation_id "$GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID" \
  '{
    _type: "https://in-toto.io/Statement/v1",
    subject: [{name: $archive_name, digest: {sha256: $archive_sha256}}],
    predicateType: "https://slsa.dev/provenance/v1",
    predicate: {
      buildDefinition: {
        buildType: "https://endlessnet.ru/build-types/stun-linux-systemd-archive/v1",
        externalParameters: {commit_sha: $commit_sha},
        internalParameters: {
          producer_repository: $producer_repository,
          producer_workflow: $producer_workflow,
          producer_run_id: ($producer_run_id | tonumber),
          commit_ci_run_id: ($commit_ci_run_id | tonumber),
          publisher_workflow_commit: $publisher_workflow_commit,
          publisher_ci_run_id: ($publisher_ci_run_id | tonumber)
        },
        resolvedDependencies: [{
          uri: ("git+https://github.com/" + $producer_repository),
          digest: {gitCommit: $commit_sha}
        }]
      },
      runDetails: {
        builder: {
          id: ("https://github.com/" + $producer_repository + "/" + $producer_workflow + "@refs/heads/main")
        },
        metadata: {invocationId: $invocation_id}
      }
    }
  }' >"$provenance"

{
  echo "archive=$archive"
  echo "checksum=$checksum"
  echo "manifest=$manifest"
  echo "provenance=$provenance"
  echo "provenance_bundle=$provenance_bundle"
  echo "provenance_reference=$(basename "$provenance")#$(basename "$provenance_bundle")"
  echo "artifact_digest=sha256:$archive_sha256"
} >>"$GITHUB_OUTPUT"
