#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_root=${WORK_ROOT:-"$(dirname "$repo_root")"}
package_repo=${ETHEREUM_PACKAGE:-"$work_root/ethereum-package-state"}
run_root=${RUN_ROOT:-"$repo_root/campaign-runs/ethereum"}
duration=${DURATION:-4m}
args_template=${ARGS_TEMPLATE:-"$repo_root/ethereum/network_params.yaml"}
mkdir -p "$run_root"

while true; do
  started=$(date -u +%Y%m%dT%H%M%SZ)
  started_lower=$(printf '%s' "$started" | tr '[:upper:]' '[:lower:]')
  seed=${SEED:-$((10#$(date -u +%s) ^ RANDOM << 15 ^ RANDOM))}
  enclave="ethereum-state-${started_lower}-${seed}"
  evidence="$run_root/$enclave"
  mkdir -p "$evidence"
  sed "s/--seed=[0-9][0-9]*/--seed=$seed/" "$args_template" >"$evidence/args.yaml"
  jq -n \
    --arg event started \
    --arg chain ethereum \
    --arg enclave "$enclave" \
    --arg started "$started" \
    --argjson seed "$seed" \
    --arg duration "$duration" \
    '{event:$event,chain:$chain,enclave:$enclave,started:$started,seed:$seed,duration:$duration}' \
    | tee "$evidence/status.json"

  set +e
  kurtosis run --enclave "$enclave" "$package_repo" \
    --args-file "$evidence/args.yaml" 2>&1 | tee "$evidence/kurtosis.log"
  launch_result=${PIPESTATUS[0]}
  set -e
  if (( launch_result == 0 )); then
    baseline_url=$(kurtosis port print "$enclave" el-1-reth-lighthouse rpc)
    candidate_url=$(kurtosis port print "$enclave" el-2-reth-lighthouse rpc)
    control_url=$(kurtosis port print "$enclave" el-3-geth-lighthouse rpc)
    [[ "$baseline_url" == *://* ]] || baseline_url="http://$baseline_url"
    [[ "$candidate_url" == *://* ]] || candidate_url="http://$candidate_url"
    [[ "$control_url" == *://* ]] || control_url="http://$control_url"
    chain_hex=$(curl --fail --silent --show-error \
      --header 'content-type: application/json' \
      --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' \
      "$baseline_url" | jq -er .result)
    chain_id=$((chain_hex))

    # Kurtosis exposes RPC before beacon genesis. Start the timed campaign only once both
    # execution clients have actually imported a post-genesis block.
    ready=0
    for _ in $(seq 1 60); do
      baseline_head=$(curl --fail --silent --show-error \
        --header 'content-type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
        "$baseline_url" | jq -er .result)
      candidate_head=$(curl --fail --silent --show-error \
        --header 'content-type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
        "$candidate_url" | jq -er .result)
      control_head=$(curl --fail --silent --show-error \
        --header 'content-type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
        "$control_url" | jq -er .result)
      if (( baseline_head > 0 && candidate_head > 0 && control_head > 0 )); then
        ready=1
        break
      fi
      sleep 2
    done
    if (( ready == 0 )); then
      echo "execution clients did not advance after beacon genesis" | tee "$evidence/oracle.jsonl"
      result=1
    fi

    if (( ready == 1 )); then
    sed "s/^chain_id: .*/chain_id: $chain_id/" \
      "$repo_root/ethereum/txgen-mix.yaml" >"$evidence/txgen-mix.yaml"
    docker run --rm --network host \
      --volume "$evidence/txgen-mix.yaml:/workload.yaml:ro" \
      --entrypoint /usr/bin/bash txgen-differential:local -c \
      "set -o pipefail; txgen-ethereum generate --spec /workload.yaml --duration $duration --seed $seed --rpc $baseline_url | bench send --rpc-url $baseline_url --tps 10 --max-concurrent 64 --retries 3 --report console" \
      >"$evidence/txgen.log" 2>&1 &
    txgen_pid=$!
    set +e
    docker run --rm --network host \
      --entrypoint /usr/local/bin/evm-rpc-oracle tx-fuzz-oracles:local \
      --left-rpc="$baseline_url" \
      --right-rpc="$control_url" \
      --head-tag=latest \
      --duration="$duration" \
      --min-blocks=32 \
      --max-lag=4 \
      --stall-timeout=30s >"$evidence/geth-control-oracle.jsonl" 2>&1 &
    control_pid=$!
    docker run --rm --network host \
      --entrypoint /usr/local/bin/evm-rpc-oracle tx-fuzz-oracles:local \
      --left-rpc="$baseline_url" \
      --right-rpc="$candidate_url" \
      --head-tag=latest \
      --duration="$duration" \
      --min-blocks=32 \
      --max-lag=4 \
      --stall-timeout=30s 2>&1 | tee "$evidence/oracle.jsonl"
    result=${PIPESTATUS[0]}
    if (( result != 0 )); then
      kill "$txgen_pid" 2>/dev/null || true
      kill "$control_pid" 2>/dev/null || true
    fi
    wait "$txgen_pid"
    txgen_result=$?
    wait "$control_pid"
    control_result=$?
    set -e
    if (( result == 0 && txgen_result != 0 )); then
      result=$txgen_result
    fi
    if (( result == 0 && control_result != 0 )); then
      result=$control_result
    fi
    fi
  else
    result=$launch_result
  fi

  kurtosis enclave inspect "$enclave" >"$evidence/enclave.txt" 2>&1 || true
  kurtosis service logs --all-services --all "$enclave" >"$evidence/services.log" 2>&1 || true
  finished=$(date -u +%Y%m%dT%H%M%SZ)
  if (( result != 0 )); then
    jq -n \
      --arg event failed \
      --arg chain ethereum \
      --arg enclave "$enclave" \
      --arg finished "$finished" \
      --argjson seed "$seed" \
      --argjson exitCode "$result" \
      '{event:$event,chain:$chain,enclave:$enclave,finished:$finished,seed:$seed,exitCode:$exitCode}' \
      | tee "$evidence/status.json"
    exit "$result"
  fi

  jq -n \
    --arg event passed \
    --arg chain ethereum \
    --arg enclave "$enclave" \
    --arg finished "$finished" \
    --argjson seed "$seed" \
    '{event:$event,chain:$chain,enclave:$enclave,finished:$finished,seed:$seed}' \
    | tee "$evidence/status.json"
  kurtosis enclave rm --force "$enclave"
  unset SEED
done
