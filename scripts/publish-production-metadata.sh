#!/usr/bin/env bash
set -euo pipefail
if [[ "$RAW_PACKAGE_DIGEST" =~ ^[0-9a-f]{64}$ ]]; then
  package_digest="sha256:$RAW_PACKAGE_DIGEST"
else
  package_digest=$RAW_PACKAGE_DIGEST
fi
[[ "$package_digest" =~ ^sha256:[0-9a-f]{64}$ ]]
[[ "$ARTIFACT_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]
echo "package_digest=$package_digest" >>"$GITHUB_OUTPUT"
{
  echo '### STUN immutable production artifact'
  echo
  echo "- schema_version: 1"
  echo "- service: stun"
  echo "- commit_sha: $COMMIT_SHA"
  echo "- producer_run_id: $GITHUB_RUN_ID"
  echo "- commit_ci_run_id: $COMMIT_CI_RUN_ID"
  echo "- publisher_commit_sha: $PUBLISHER_COMMIT_SHA"
  echo "- publisher_ci_run_id: $PUBLISHER_CI_RUN_ID"
  echo "- package_digest: $package_digest"
  echo "- artifact_digest: $ARTIFACT_DIGEST"
  echo "- provenance_reference: $PROVENANCE_REFERENCE"
  echo "- sigstore_rekor_index: $SIGSTORE_REKOR_INDEX"
} >>"$GITHUB_STEP_SUMMARY"
