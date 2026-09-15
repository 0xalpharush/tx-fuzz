#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
run_root=${RUN_ROOT:-"$repo_root/campaign-runs/tempo"}
duration=${DURATION:-3m45s}
oracle_duration=${ORACLE_DURATION:-4m}
args_template=${ARGS_TEMPLATE:-"$repo_root/tempo/kurtosis/images.json"}
mkdir -p "$run_root"

while true; do
  started=$(date -u +%Y%m%dT%H%M%SZ)
  seed=${SEED:-$((10#$(date -u +%s) ^ RANDOM << 15 ^ RANDOM))}
  enclave="tempo-state-${started,,}-${seed}"
  evidence="$run_root/$enclave"
  mkdir -p "$evidence"

  jq \
    --argjson seed "$seed" \
    --arg duration "$duration" \
    --arg oracle_duration "$oracle_duration" \
    '.seed = $seed | .duration = $duration | .oracle_duration = $oracle_duration' \
    "$args_template" >"$evidence/args.json"
  jq -n \
    --arg event started \
    --arg chain tempo \
    --arg enclave "$enclave" \
    --arg started "$started" \
    --argjson seed "$seed" \
    --arg duration "$duration" \
    '{event:$event,chain:$chain,enclave:$enclave,started:$started,seed:$seed,duration:$duration}' \
    | tee "$evidence/status.json"

  set +e
  kurtosis run --enclave "$enclave" "$repo_root/tempo/kurtosis" \
    --args-file "$evidence/args.json" 2>&1 | tee "$evidence/kurtosis.log"
  result=${PIPESTATUS[0]}
  set -e

  kurtosis enclave inspect "$enclave" >"$evidence/enclave.txt" 2>&1 || true
  kurtosis service logs --all-services --all "$enclave" >"$evidence/services.log" 2>&1 || true
  finished=$(date -u +%Y%m%dT%H%M%SZ)
  if (( result != 0 )); then
    jq -n \
      --arg event failed \
      --arg chain tempo \
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
    --arg chain tempo \
    --arg enclave "$enclave" \
    --arg finished "$finished" \
    --argjson seed "$seed" \
    '{event:$event,chain:$chain,enclave:$enclave,finished:$finished,seed:$seed}' \
    | tee "$evidence/status.json"
  kurtosis enclave rm --force "$enclave"
  unset SEED
done
