#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_root=${WORK_ROOT:-"$(dirname "$repo_root")"}
baseline_repo=${TEMPO_REVM_REPO:-"$work_root/tempo-revm"}
candidate_repo=${TEMPO_EVM2_REPO:-"$work_root/tempo"}
txgen_repo=${TXGEN_REPO:-"$work_root/txgen"}
baseline_ref=${TEMPO_REVM_REF:-ca57dfdc9e808aea6c48f07978be156cb81a2b7b}
candidate_ref=${TEMPO_EVM2_REF:-dffd26048cbeb638bed66c5371e9f254845bd1a1}
txgen_ref=${TXGEN_REF:-0d62b7dbf14f338e0fc51750053d2cafe3d55bde}
dockerfile="$repo_root/tempo/kurtosis/Dockerfile.tempo-candidate"

require_ref() {
  local repo=$1 expected=$2 label=$3 actual
  actual=$(git -C "$repo" rev-parse HEAD)
  if [[ "$actual" != "$expected" ]]; then
    printf '%s must be checked out at %s (found %s)\n' "$label" "$expected" "$actual" >&2
    exit 1
  fi
}

require_ref "$baseline_repo" "$baseline_ref" Tempo/revm
require_ref "$candidate_repo" "$candidate_ref" Tempo/EVM2
require_ref "$txgen_repo" "$txgen_ref" txgen

build_validator() {
  local repo=$1 tag=$2 sha
  sha=$(git -C "$repo" rev-parse HEAD)
  docker build --file "$dockerfile" \
    --build-arg "VERGEN_GIT_SHA=$sha" \
    --build-arg "VERGEN_GIT_SHA_SHORT=${sha:0:9}" \
    --tag "$tag" "$repo"
}

build_validator "$baseline_repo" tempo-validator:revm
build_validator "$candidate_repo" tempo-validator:evm2
docker build --tag tx-fuzz-tempo:local "$repo_root/tempo"
docker build --tag txgen-differential:local "$txgen_repo"
printf 'tempo campaign images are ready: revm=%s evm2=%s txgen=%s\n' \
  "$baseline_ref" "$candidate_ref" "$txgen_ref"
