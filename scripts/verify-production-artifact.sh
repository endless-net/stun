#!/usr/bin/env bash

set -euo pipefail

: "${ARTIFACT_DIR:?ARTIFACT_DIR is required}"
: "${COMMIT_SHA:?COMMIT_SHA is required}"
: "${COMMIT_CI_RUN_ID:?COMMIT_CI_RUN_ID is required}"
: "${PRODUCER_REPOSITORY:?PRODUCER_REPOSITORY is required}"
: "${PRODUCER_RUN_ID:?PRODUCER_RUN_ID is required}"
: "${PUBLISHER_COMMIT_SHA:?PUBLISHER_COMMIT_SHA is required}"
: "${PUBLISHER_CI_RUN_ID:?PUBLISHER_CI_RUN_ID is required}"
: "${ARTIFACT_DIGEST:?ARTIFACT_DIGEST is required}"
: "${SIGSTORE_REKOR_INDEX:?SIGSTORE_REKOR_INDEX is required}"

producer_workflow=.github/workflows/ci.yml
artifact_name="endlessnet-stun-$COMMIT_SHA"
archive_name="$artifact_name.tar.gz"
archive="$ARTIFACT_DIR/$archive_name"
checksum="$archive.sha256"
publication="$ARTIFACT_DIR/$artifact_name.manifest.json"
statement="$ARTIFACT_DIR/$archive_name.intoto.json"
bundle="$ARTIFACT_DIR/$archive_name.provenance.sigstore.json"
certificate_identity="https://github.com/$PRODUCER_REPOSITORY/$producer_workflow@refs/heads/main"
certificate_issuer=https://token.actions.githubusercontent.com

[[ "$COMMIT_SHA" =~ ^[0-9a-f]{40}$ ]]
[[ "$COMMIT_CI_RUN_ID" =~ ^[1-9][0-9]*$ ]]
[[ "$PRODUCER_RUN_ID" =~ ^[1-9][0-9]*$ ]]
[[ "$PUBLISHER_COMMIT_SHA" =~ ^[0-9a-f]{40}$ ]]
[[ "$PUBLISHER_CI_RUN_ID" =~ ^[1-9][0-9]*$ ]]
[[ "$ARTIFACT_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]
[[ "$SIGSTORE_REKOR_INDEX" =~ ^[1-9][0-9]*$ ]]
test "$PRODUCER_REPOSITORY" = endless-net/stun
test -f "$archive" -a ! -L "$archive"
test -f "$checksum" -a ! -L "$checksum"
test -f "$publication" -a ! -L "$publication"
test -f "$statement" -a ! -L "$statement"
test -f "$bundle" -a ! -L "$bundle"

expected=${ARTIFACT_DIGEST#sha256:}
grep -Fx "$expected  $archive_name" "$checksum" >/dev/null
actual=$(sha256sum "$archive" | awk '{print $1}')
test "$actual" = "$expected"

while IFS= read -r entry; do
  case "$entry" in
    /*|../*|*/../*|*/..)
      echo "unsafe path in STUN production archive: $entry" >&2
      exit 1
      ;;
  esac
done < <(tar -tzf "$archive")

stage=$(mktemp -d "${TMPDIR:-/tmp}/endlessnet-stun-artifact.XXXXXX")
cleanup() {
  rm -rf -- "$stage"
}
trap cleanup EXIT HUP INT TERM
tar -xzf "$archive" -C "$stage"
test -x "$stage/bin/linux-amd64/endlessnet-stun"
test -x "$stage/bin/linux-amd64/endlessnet-stun-smoke"
test -x "$stage/bin/linux-arm64/endlessnet-stun"
test -x "$stage/bin/linux-arm64/endlessnet-stun-smoke"
test -f "$stage/systemd/endlessnet-stun.service"
test -f "$stage/LICENSE"
test -f "$stage/NOTICE"
test -f "$stage/THIRD_PARTY_NOTICES"
test -z "$(find "$stage" -type l -print -quit)"

actual_layout=$(cd "$stage" && find . -mindepth 1 -printf '%P\n' | LC_ALL=C sort)
expected_layout=$(printf '%s\n' \
  LICENSE \
  NOTICE \
  THIRD_PARTY_NOTICES \
  bin \
  bin/linux-amd64 \
  bin/linux-amd64/endlessnet-stun \
  bin/linux-amd64/endlessnet-stun-smoke \
  bin/linux-arm64 \
  bin/linux-arm64/endlessnet-stun \
  bin/linux-arm64/endlessnet-stun-smoke \
  systemd \
  systemd/endlessnet-stun.service)
test "$actual_layout" = "$expected_layout"

jq -e \
  --arg repository "$PRODUCER_REPOSITORY" \
  --arg workflow "$producer_workflow" \
  --arg run_id "$PRODUCER_RUN_ID" \
  --arg commit_ci_run_id "$COMMIT_CI_RUN_ID" \
  --arg publisher_commit "$PUBLISHER_COMMIT_SHA" \
  --arg publisher_ci_run_id "$PUBLISHER_CI_RUN_ID" \
  --arg commit_sha "$COMMIT_SHA" \
  --arg artifact_name "$artifact_name" \
  --arg artifact_digest "$ARTIFACT_DIGEST" \
  --arg statement_name "$archive_name.intoto.json" \
  --arg bundle_name "$archive_name.provenance.sigstore.json" \
  --arg certificate_identity "$certificate_identity" \
  --arg certificate_issuer "$certificate_issuer" \
  --arg rekor_index "$SIGSTORE_REKOR_INDEX" '
    (keys == [
      "architectures",
      "artifact_digest",
      "artifact_name",
      "commit_ci_run_id",
      "commit_sha",
      "created_at",
      "producer_repository",
      "producer_run_id",
      "producer_workflow",
      "provenance",
      "publisher_ci_run_id",
      "publisher_workflow_commit",
      "schema_version",
      "service"
    ]) and
    .schema_version == 1 and
    .service == "stun" and
    .producer_repository == $repository and
    .producer_workflow == $workflow and
    .producer_run_id == ($run_id | tonumber) and
    .commit_ci_run_id == ($commit_ci_run_id | tonumber) and
    .publisher_workflow_commit == $publisher_commit and
    .publisher_ci_run_id == ($publisher_ci_run_id | tonumber) and
    .commit_sha == $commit_sha and
    .artifact_name == $artifact_name and
    .artifact_digest == $artifact_digest and
    .architectures == ["amd64", "arm64"] and
    (.created_at | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$")) and
    (.provenance | keys == [
      "certificate_identity",
      "certificate_issuer",
      "rekor_log_index",
      "sigstore_bundle",
      "statement"
    ]) and
    .provenance.statement == $statement_name and
    .provenance.sigstore_bundle == $bundle_name and
    .provenance.certificate_identity == $certificate_identity and
    .provenance.certificate_issuer == $certificate_issuer and
    (.provenance.rekor_log_index | tostring) == $rekor_index
  ' "$publication" >/dev/null

jq -e \
  --arg archive_name "$archive_name" \
  --arg archive_sha256 "$expected" \
  --arg repository "$PRODUCER_REPOSITORY" \
  --arg workflow "$producer_workflow" \
  --arg run_id "$PRODUCER_RUN_ID" \
  --arg commit_ci_run_id "$COMMIT_CI_RUN_ID" \
  --arg publisher_commit "$PUBLISHER_COMMIT_SHA" \
  --arg publisher_ci_run_id "$PUBLISHER_CI_RUN_ID" \
  --arg commit_sha "$COMMIT_SHA" \
  --arg invocation_id "https://github.com/$PRODUCER_REPOSITORY/actions/runs/$PRODUCER_RUN_ID" \
  --arg builder_id "$certificate_identity" '
    (keys == ["_type", "predicate", "predicateType", "subject"]) and
    ._type == "https://in-toto.io/Statement/v1" and
    .predicateType == "https://slsa.dev/provenance/v1" and
    (.subject | length) == 1 and
    .subject[0].name == $archive_name and
    .subject[0].digest.sha256 == $archive_sha256 and
    .predicate.buildDefinition.buildType == "https://endlessnet.ru/build-types/stun-linux-systemd-archive/v1" and
    .predicate.buildDefinition.externalParameters.commit_sha == $commit_sha and
    .predicate.buildDefinition.internalParameters.producer_repository == $repository and
    .predicate.buildDefinition.internalParameters.producer_workflow == $workflow and
    .predicate.buildDefinition.internalParameters.producer_run_id == ($run_id | tonumber) and
    .predicate.buildDefinition.internalParameters.commit_ci_run_id == ($commit_ci_run_id | tonumber) and
    .predicate.buildDefinition.internalParameters.publisher_workflow_commit == $publisher_commit and
    .predicate.buildDefinition.internalParameters.publisher_ci_run_id == ($publisher_ci_run_id | tonumber) and
    any(.predicate.buildDefinition.resolvedDependencies[];
      .uri == ("git+https://github.com/" + $repository) and
      .digest.gitCommit == $commit_sha) and
    .predicate.runDetails.builder.id == $builder_id and
    .predicate.runDetails.metadata.invocationId == $invocation_id
  ' "$statement" >/dev/null

jq -e --arg rekor_index "$SIGSTORE_REKOR_INDEX" '
  if .verificationMaterial.tlogEntries? then
    ([.verificationMaterial.tlogEntries[] |
      select((.logIndex | tostring) == $rekor_index)] | length == 1)
  elif .rekorBundle.Payload.logIndex? then
    (.rekorBundle.Payload.logIndex | tostring) == $rekor_index
  else
    false
  end
' "$bundle" >/dev/null
