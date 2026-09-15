#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
run_root=${RUN_ROOT:-"$repo_root/campaign-runs/tempo"}
duration=${DURATION:-4m}
mkdir -p "$run_root"

while true; do
  started=$(date -u +%Y%m%dT%H%M%SZ)
  started_lower=$(printf '%s' "$started" | tr '[:upper:]' '[:lower:]')
  seed=${SEED:-$((10#$(date -u +%s) ^ RANDOM << 15 ^ RANDOM))}
  run="tempo-state-${started_lower}-${seed}"
  network="$run"
  revm_name="${run}-revm"
  evm2_name="${run}-evm2"
  evidence="$run_root/$run"
  config="$evidence/localnet"
  subnet_octet=$((20 + seed % 200))
  subnet="10.${subnet_octet}.0.0/24"
  revm_ip="10.${subnet_octet}.0.2"
  evm2_ip="10.${subnet_octet}.0.3"
  mkdir -p "$config"

  cleanup() {
    docker rm --force "$revm_name" "$evm2_name" >/dev/null 2>&1 || true
    docker network rm "$network" >/dev/null 2>&1 || true
  }
  trap cleanup EXIT INT TERM

  docker network create --subnet "$subnet" "$network" >/dev/null
  docker run --rm --volume "$config:/output" \
    --entrypoint /usr/local/bin/tempo-xtask tempo-validator:revm \
    generate-localnet --output /output \
    --validators "$revm_ip:8000,$evm2_ip:8000" \
    --accounts 1000 --epoch-length 64 --seed "$seed" \
    >"$evidence/generate-localnet.log" 2>&1

  revm_dir="$revm_ip:8000"
  evm2_dir="$evm2_ip:8000"
  revm_id=$(<"$config/$revm_dir/enode.identity")
  evm2_id=$(<"$config/$evm2_dir/enode.identity")
  trusted="enode://${revm_id}@${revm_ip}:8001,enode://${evm2_id}@${evm2_ip}:8001"

  start_validator() {
    local name=$1 alias=$2 image=$3 ip=$4 validator_dir=$5
    shift 5
    docker run --detach --name "$name" --network "$network" --network-alias "$alias" --ip "$ip" \
      --volume "$config:/config:ro" \
      --volume "$repo_root/tempo/kurtosis/localnet-secret:/secret:ro" \
      --entrypoint /usr/local/bin/tempo "$image" node \
      --chain /config/genesis.json --datadir /data \
      --consensus.signing-key "/config/$validator_dir/signing.key" \
      --consensus.secret /secret \
      --consensus.signing-share "/config/$validator_dir/signing.share" \
      --consensus.listen-address 0.0.0.0:8000 \
      --consensus.metrics-address 0.0.0.0:8001 \
      --consensus.use-local-defaults --consensus.bypass-ip-check \
      --trusted-peers "$trusted" --port 8001 --discovery.port 8001 \
      --p2p-secret-key "/config/$validator_dir/enode.key" \
      --tempo.bootnodes-endpoint none \
      --http --http.addr 0.0.0.0 --http.port 8545 --http.api all \
      --builder.gaslimit 3000000000 "$@" >/dev/null
  }

  start_validator "$revm_name" tempo-revm tempo-validator:revm "$revm_ip" "$revm_dir" \
    --faucet.enabled \
    --faucet.private-key ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 \
    --faucet.amount 1000000000000000 \
    --faucet.node-address http://127.0.0.1:8545 \
    --faucet.address 0x20c0000000000000000000000000000000000000 \
    0x20c0000000000000000000000000000000000001 \
    0x20c0000000000000000000000000000000000002 \
    0x20c0000000000000000000000000000000000003
  start_validator "$evm2_name" tempo-evm2 tempo-validator:evm2 "$evm2_ip" "$evm2_dir"

  rpc="http://tempo-revm:8545"
  candidate_rpc="http://tempo-evm2:8545"
  ready=0
  for _ in $(seq 1 60); do
    if docker run --rm --network "$network" --entrypoint /usr/local/bin/tempo-fund \
      tx-fuzz-tempo:local --rpc "$rpc" \
      --addresses f39fd6e51aad88f6f4ce6ab8827279cfffb92266,70997970c51812dc3a010c7d01b50e0d17dc79c8,3c44cdddb6a900fa2b585dd299e03d12fa4293bc,90f79bf6eb2c4f870365e785982e1f101e93b906,15d34aaf54267db7d7c367839aaf71a00a2c6a65 \
      --timeout 20s >"$evidence/fund.log" 2>&1; then
      ready=1
      break
    fi
    sleep 2
  done
  if (( ready == 0 )); then
    docker logs "$revm_name" >"$evidence/revm.log" 2>&1 || true
    docker logs "$evm2_name" >"$evidence/evm2.log" 2>&1 || true
    exit 1
  fi

  docker run --rm --network "$network" \
    --env TXGEN_ACCOUNTS=10 \
    --env 'TXGEN_TIP20_TOKENS=["0x20c0000000000000000000000000000000000000","0x20c0000000000000000000000000000000000001","0x20c0000000000000000000000000000000000002","0x20c0000000000000000000000000000000000003"]' \
    --entrypoint /bin/sh \
    txgen-differential:local -c \
    "txgen-tempo generate --defer-signing --spec /specs/tempo-bench/presets/mix.yml --duration $duration --seed $seed --rpc $rpc | bench send --rpc-url $rpc --tps 10 --late-signing-spec /specs/tempo-bench/presets/mix.yml --max-concurrent 64 --retries 3 --report console" \
    >"$evidence/txgen.log" 2>&1 &
  txgen_pid=$!
  docker run --rm --network "$network" --entrypoint /usr/local/bin/tempo-evm-diff \
    tx-fuzz-tempo:local --single-rpc "$rpc" --seed "$seed" \
    --programs 64 --max-code-bytes 512 --timeout "$duration" \
    >"$evidence/tx-fuzz.log" 2>&1 &
  fuzz_pid=$!

  set +e
  docker run --rm --network "$network" --entrypoint /usr/local/bin/evm-rpc-oracle \
    tx-fuzz-tempo:local --left-rpc "$rpc" --right-rpc "$candidate_rpc" \
    --chain-id 1337 --head-tag latest --duration "$duration" \
    --min-blocks 64 --max-lag 10 --stall-timeout 30s \
    2>&1 | tee "$evidence/oracle.jsonl"
  result=${PIPESTATUS[0]}
  wait "$txgen_pid" || result=$?
  wait "$fuzz_pid" || result=$?
  set -e

  docker logs "$revm_name" >"$evidence/revm.log" 2>&1 || true
  docker logs "$evm2_name" >"$evidence/evm2.log" 2>&1 || true
  if ! grep -q '"outcome":"accepted"' "$evidence/tx-fuzz.log"; then
    echo "tx-fuzz did not land an accepted randomized program" >&2
    result=1
  fi
  for feature in ethereum-dynamic-fee ethereum-legacy tempo-plain batch-tip20-evm \
    parallel-nonce fee-token fee-sponsored tempo-authorization; do
    if ! grep -q "\"tempoFeature\":\"$feature\"" "$evidence/tx-fuzz.log"; then
      echo "tx-fuzz did not exercise $feature" >&2
      result=1
    fi
  done
  if ! awk '/"tempoFeature":"ethereum-/ && /"outcome":"(accepted|reverted)"/ { found=1 } END { exit !found }' "$evidence/tx-fuzz.log" || \
    ! awk '/"tempoFeature":"(tempo-|batch-|parallel-|fee-)/ && /"outcome":"(accepted|reverted)"/ { found=1 } END { exit !found }' "$evidence/tx-fuzz.log"; then
    echo "tx-fuzz did not mine both Ethereum and Tempo envelope families" >&2
    result=1
  fi
  if ! grep -q 'constructed proposal' "$evidence/revm.log" || \
    ! grep -q 'constructed proposal' "$evidence/evm2.log"; then
    echo "both engines did not exercise proposal construction" >&2
    result=1
  fi
  cleanup
  trap - EXIT INT TERM
  if (( result != 0 )); then
    exit "$result"
  fi
  unset SEED
done
