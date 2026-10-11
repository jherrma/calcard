#!/bin/bash
# Build and push the multi-arch (linux/amd64 + linux/arm64) image to Docker Hub.
#
#   scripts/docker-release.sh [version] [--no-push]
#
# version defaults to the git tag on HEAD with the leading "v" stripped
# (v1.0.0-rc1 -> 1.0.0-rc1). Stable versions (no "-suffix") also get :latest;
# pre-releases never do. arm64 is built under QEMU, so it is slow.
# Requires `docker login` for the push. --no-push builds both platforms only,
# to verify they compile.
set -euo pipefail

IMAGE="${IMAGE:-herrj/calcard}"
PLATFORMS="linux/amd64,linux/arm64"

cd "$(dirname "$0")/../.."   # repo root: the build context

VERSION=""
PUSH=1
for arg in "$@"; do
  case "$arg" in
    --no-push) PUSH=0 ;;
    *) VERSION="${arg#v}" ;;
  esac
done
if [ -z "$VERSION" ]; then
  VERSION="$(git describe --tags --exact-match 2>/dev/null)" \
    || { echo "HEAD is not on a tag; pass a version explicitly" >&2; exit 1; }
  VERSION="${VERSION#v}"
fi

TAGS=(-t "$IMAGE:$VERSION")
[[ "$VERSION" != *-* ]] && TAGS+=(-t "$IMAGE:latest")

# The default "docker" driver cannot build multi-platform images.
docker buildx inspect multiarch >/dev/null 2>&1 \
  || docker buildx create --name multiarch --driver docker-container >/dev/null
docker buildx use multiarch

OUT=(--push)
[ "$PUSH" = 1 ] || OUT=()

echo "Building $IMAGE:$VERSION for $PLATFORMS (push=$PUSH)"
docker buildx build --platform "$PLATFORMS" -f server/Dockerfile "${TAGS[@]}" "${OUT[@]}" .
