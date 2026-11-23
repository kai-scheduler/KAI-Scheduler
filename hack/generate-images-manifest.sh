#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# Generate a flat images.yaml manifest listing every pushed image (component x
# FIPS variant x platform) for a release. Requires the caller to already be
# logged in to the registry (docker login) so `docker buildx imagetools
# inspect` can read the pushed manifest lists.
#
# Usage: hack/generate-images-manifest.sh vX.Y.Z <registry> "<component1> <component2> ..." > images.yaml
set -euo pipefail

VERSION="${1:-}"
REGISTRY="${2:-}"
COMPONENTS="${3:-}"
if [ -z "$VERSION" ] || [ -z "$REGISTRY" ] || [ -z "$COMPONENTS" ]; then
  echo "usage: $0 vX.Y.Z <registry> \"<space-separated components>\"" >&2
  exit 1
fi

echo "version: ${VERSION}"
echo "images:"
for component in $COMPONENTS; do
  for tag in "${VERSION}" "${VERSION}-fips"; do
    ref="${REGISTRY}/${component}:${tag}"
    docker buildx imagetools inspect "$ref" --raw | jq -r --arg component "$component" --arg uri "$ref" '
      .manifests[]
      | select(.platform.os == "linux")
      | select(.platform.architecture == "amd64" or .platform.architecture == "arm64")
      | "  - component: \($component)\n    os: \(.platform.os)\n    arch: \(.platform.architecture)\n    uri: \($uri)\n    digest: \(.digest)"
    '
  done
done
