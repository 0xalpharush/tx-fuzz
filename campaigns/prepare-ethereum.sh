#!/usr/bin/env bash
set -euo pipefail

script_repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_root=${WORK_ROOT:-"$(dirname "$script_repo")"}
reth_repo="$work_root/reth"
package_repo="$work_root/ethereum-package-current"
tx_fuzz_repo=${TX_FUZZ_REPO:-"$script_repo"}
revm_ref=${RETH_REVM_REF:-8a993c0327d92a0e96ec37a021f5ab806026b885}
evm2_ref=${RETH_EVM2_REF:-39b99d8a1594b3417263e67d503b4a1e908a068c}
package_ref=${ETHEREUM_PACKAGE_REF:-c0db06b29b8266e65c9b80b64895e07058d28d0b}

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
  git clone --filter=blob:none https://github.com/ethpandaops/ethereum-package.git "$package_repo"
fi
git -C "$package_repo" fetch origin "$package_ref"
git -C "$package_repo" checkout --detach "$package_ref"

printf 'ethereum campaign images and package are ready\n'
