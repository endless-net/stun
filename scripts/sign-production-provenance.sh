#!/usr/bin/env bash
set -euo pipefail
certificate_identity="https://github.com/$GITHUB_REPOSITORY/.github/workflows/ci.yml@refs/heads/main"
certificate_issuer=https://token.actions.githubusercontent.com
cosign sign-blob --yes --bundle "$PROVENANCE_BUNDLE" "$PROVENANCE"
cosign verify-blob \
  --bundle "$PROVENANCE_BUNDLE" \
  --certificate-identity "$certificate_identity" \
  --certificate-oidc-issuer "$certificate_issuer" \
  "$PROVENANCE"
sigstore_rekor_index=$(jq -r '
  if .verificationMaterial.tlogEntries? then
    .verificationMaterial.tlogEntries[0].logIndex
  elif .rekorBundle.Payload.logIndex? then
    .rekorBundle.Payload.logIndex
  else
    empty
  end
' "$PROVENANCE_BUNDLE")
[[ "$sigstore_rekor_index" =~ ^[1-9][0-9]*$ ]]
updated="$RUNNER_TEMP/publication-manifest.json"
jq \
  --arg provenance_statement "$(basename "$PROVENANCE")" \
  --arg provenance_bundle "$(basename "$PROVENANCE_BUNDLE")" \
  --arg certificate_identity "$certificate_identity" \
  --arg certificate_issuer "$certificate_issuer" \
  --arg sigstore_rekor_index "$sigstore_rekor_index" \
  '. + {provenance: {
    statement: $provenance_statement,
    sigstore_bundle: $provenance_bundle,
    certificate_identity: $certificate_identity,
    certificate_issuer: $certificate_issuer,
    rekor_log_index: ($sigstore_rekor_index | tonumber)
  }}' "$MANIFEST" >"$updated"
mv "$updated" "$MANIFEST"
echo "sigstore_rekor_index=$sigstore_rekor_index" >>"$GITHUB_OUTPUT"
