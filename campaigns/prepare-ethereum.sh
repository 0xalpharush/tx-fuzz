#!/usr/bin/env bash
set -euo pipefail

script_repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_root=${WORK_ROOT:-"$(dirname "$script_repo")"}
reth_repo="$work_root/reth"
package_repo="$work_root/ethereum-package-state"
tx_fuzz_repo=${TX_FUZZ_REPO:-"$script_repo"}
revm_ref=${RETH_REVM_REF:-8a993c0327d92a0e96ec37a021f5ab806026b885}
evm2_ref=${RETH_EVM2_REF:-39b99d8a1594b3417263e67d503b4a1e908a068c}
package_ref=${ETHEREUM_PACKAGE_REF:-195fcb9093b7dd6c8d35aaf4a0f7bc956f2f1fa9}

mkdir -p "$work_root"
if [[ ! -d "$reth_repo/.git" ]]; then
  git clone --filter=blob:none https://github.com/paradigmxyz/reth.git "$reth_repo"
fi
git -C "$reth_repo" fetch https://github.com/0xalpharush/reth.git \
  "$evm2_ref"
git -C "$reth_repo" fetch origin "$revm_ref"

git -C "$reth_repo" checkout --detach "$revm_ref"
docker build \
  --file "$tx_fuzz_repo/ethereum/Dockerfile.reth" \
  --build-arg BUILD_PROFILE=release \
  --build-arg 'RUSTFLAGS=-C lto=off -C codegen-units=64' \
  --build-arg VERGEN_GIT_SHA="$revm_ref" \
  --build-arg VERGEN_GIT_DESCRIBE="$revm_ref" \
  --tag reth-revm:local "$reth_repo"

git -C "$reth_repo" checkout --detach "$evm2_ref"
docker build \
  --file "$tx_fuzz_repo/ethereum/Dockerfile.reth" \
  --build-arg BUILD_PROFILE=release \
  --build-arg 'RUSTFLAGS=-C lto=off -C codegen-units=64' \
  --build-arg VERGEN_GIT_SHA="$evm2_ref" \
  --build-arg VERGEN_GIT_DESCRIBE="$evm2_ref" \
  --tag reth-evm2:local "$reth_repo"

docker build --tag tx-fuzz-ethereum:local "$tx_fuzz_repo"
docker build --tag txgen-differential:local "$work_root/txgen"
docker build --tag tx-fuzz-oracles:local "$tx_fuzz_repo/tempo"

if [[ ! -d "$package_repo/.git" ]]; then
  git clone --filter=blob:none https://github.com/flashbots/kurtosis-ethereum-package.git "$package_repo"
fi
git -C "$package_repo" fetch origin "$package_ref"
git -C "$package_repo" checkout --detach "$package_ref"
if git -C "$package_repo" apply --unidiff-zero --check \
  "$tx_fuzz_repo/ethereum/kurtosis-ethereum-package.patch"; then
  git -C "$package_repo" apply --unidiff-zero \
    "$tx_fuzz_repo/ethereum/kurtosis-ethereum-package.patch"
elif ! git -C "$package_repo" apply --unidiff-zero --reverse --check \
  "$tx_fuzz_repo/ethereum/kurtosis-ethereum-package.patch"; then
  echo "Kurtosis Ethereum package does not match the pinned patch" >&2
  exit 1
fi

printf 'ethereum campaign images and package are ready\n'
