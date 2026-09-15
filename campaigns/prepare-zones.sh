#!/usr/bin/env bash
set -euo pipefail

script_repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_root=${WORK_ROOT:-"$(dirname "$script_repo")"}
zones_repo=${ZONES_REPO:-"$work_root/zones"}
txgen_repo=${TXGEN_REPO:-"$work_root/txgen"}
tempo_repo=${TEMPO_TIP1098_REPO:-"$work_root/tempo-tip1098"}
zones_ref=${ZONES_REF:-1aaabe19b10703c42324ccce4c19a7336f227b21}
tempo_ref=${TEMPO_TIP1098_REF:-15b79e0c63d0ac98f5b85cae2ba858155b5301d3}
txgen_ref=${TXGEN_REF:-0d62b7dbf14f338e0fc51750053d2cafe3d55bde}

if [[ ! -e "$zones_repo/.git" ]]; then
  git clone https://github.com/tempoxyz/zones.git "$zones_repo"
fi
git -C "$zones_repo" fetch https://github.com/0xalpharush/zones.git "$zones_ref"
git -C "$zones_repo" checkout --detach "$zones_ref"
build_patch="$script_repo/zones/docker-build-jobs.patch"
if git -C "$zones_repo" apply --check "$build_patch"; then
  git -C "$zones_repo" apply "$build_patch"
elif ! git -C "$zones_repo" apply --reverse --check "$build_patch"; then
  echo "Zones checkout does not match the pinned build patch" >&2
  exit 1
fi
foundry_patch="$script_repo/zones/foundry-worktree.patch"
if git -C "$zones_repo" apply --unidiff-zero --check "$foundry_patch"; then
  git -C "$zones_repo" apply --unidiff-zero "$foundry_patch"
elif ! git -C "$zones_repo" apply --unidiff-zero --reverse --check "$foundry_patch"; then
  echo "Zones checkout does not match the pinned Foundry patch" >&2
  exit 1
fi

if [[ ! -e "$tempo_repo/.git" ]]; then
  git clone https://github.com/tempoxyz/tempo.git "$tempo_repo"
fi
git -C "$tempo_repo" fetch https://github.com/0xalpharush/tempo.git "$tempo_ref"
git -C "$tempo_repo" checkout --detach "$tempo_ref"
tempo_build_patch="$script_repo/zones/tempo-docker-build-jobs.patch"
if git -C "$tempo_repo" apply --check "$tempo_build_patch"; then
  git -C "$tempo_repo" apply "$tempo_build_patch"
elif ! git -C "$tempo_repo" apply --reverse --check "$tempo_build_patch"; then
  echo "Tempo TIP-1098 checkout does not match the pinned build patch" >&2
  exit 1
fi

if [[ "$(git -C "$txgen_repo" rev-parse HEAD)" != "$txgen_ref" ]]; then
  printf 'txgen must be checked out at %s\n' "$txgen_ref" >&2
  exit 1
fi

(cd "$tempo_repo" && \
  VERGEN_GIT_SHA="$tempo_ref" VERGEN_GIT_SHA_SHORT="${tempo_ref:0:7}" docker buildx bake \
  --file docker-bake.hcl \
  --load \
  --set "*.platform=linux/amd64" \
  --set "tempo.tags=tempo-tip1098:local" \
  tempo)

(cd "$zones_repo" && \
  VERGEN_GIT_SHA="$zones_ref" VERGEN_GIT_SHA_SHORT="${zones_ref:0:7}" docker buildx bake \
  --file docker/docker-bake.hcl \
  --load \
  --set "*.platform=linux/amd64" \
  --set "tempo-zone.tags=tempo-zone:prover-latest" \
  --set "tempo-zone-xtask.tags=tempo-zone-xtask:prover-latest" \
  --set "tempo-zone-prover-utils.tags=tempo-zone-prover-utils:prover-latest" \
  tempo-zone tempo-zone-xtask tempo-zone-prover-utils)
docker build --tag txgen-differential:local "$txgen_repo"
docker build --tag tx-fuzz-tempo:local "$script_repo/tempo"

printf 'zones campaign images are ready: zones=%s tempo=%s txgen=%s\n' \
  "$zones_ref" "$tempo_ref" "$txgen_ref"
