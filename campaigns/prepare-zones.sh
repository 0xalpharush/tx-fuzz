#!/usr/bin/env bash
set -euo pipefail

script_repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_root=${WORK_ROOT:-"$(dirname "$script_repo")"}
zones_repo=${ZONES_REPO:-"$work_root/zones"}
txgen_repo=${TXGEN_REPO:-"$work_root/txgen"}
tempo_repo=${TEMPO_TIP1098_REPO:-"$work_root/tempo-tip1098"}
zones_ref=${ZONES_REF:-0b3f7d00fa4e4e44494efdc5a6052fdd558f13d3}
tempo_ref=${TEMPO_TIP1098_REF:-15b79e0c63d0ac98f5b85cae2ba858155b5301d3}

if [[ ! -e "$zones_repo/.git" ]]; then
  git clone https://github.com/tempoxyz/zones.git "$zones_repo"
fi
git -C "$zones_repo" fetch https://github.com/0xalpharush/zones.git "$zones_ref"
git -C "$zones_repo" checkout --detach "$zones_ref"

if [[ ! -e "$tempo_repo/.git" ]]; then
  git clone https://github.com/tempoxyz/tempo.git "$tempo_repo"
fi
git -C "$tempo_repo" fetch https://github.com/0xalpharush/tempo.git "$tempo_ref"
git -C "$tempo_repo" checkout --detach "$tempo_ref"

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
  --set "tempo-zone-prover-utils.tags=tempo-zone-prover-utils:prover-latest" \
  tempo-zone tempo-zone-prover-utils)
docker build --tag txgen-differential:local "$txgen_repo"

printf 'zones campaign images are ready: zones=%s tempo=%s\n' "$zones_ref" "$tempo_ref"
