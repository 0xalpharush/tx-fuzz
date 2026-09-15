#!/usr/bin/env bash
set -euo pipefail

baseline_url=$1
candidate_url=$2
minimum_block=${3:-32}

rpc_block() {
  curl --fail --silent --show-error \
    --header 'content-type: application/json' \
    --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["finalized",false]}' \
    "$1"
}

baseline=$(rpc_block "$baseline_url")
candidate=$(rpc_block "$candidate_url")

for field in number hash stateRoot receiptsRoot; do
  baseline_value=$(jq -er ".result.${field}" <<<"$baseline")
  candidate_value=$(jq -er ".result.${field}" <<<"$candidate")
  if [[ "$baseline_value" != "$candidate_value" ]]; then
    echo "finalized ${field} divergence: revm=${baseline_value} evm2=${candidate_value}" >&2
    exit 1
  fi
done

number_hex=$(jq -er '.result.number' <<<"$baseline")
number=$((number_hex))
if (( number < minimum_block )); then
  echo "campaign did not finalize enough blocks: got ${number}, need ${minimum_block}" >&2
  exit 1
fi

jq -n \
  --argjson block "$number" \
  --arg hash "$(jq -r '.result.hash' <<<"$baseline")" \
  --arg stateRoot "$(jq -r '.result.stateRoot' <<<"$baseline")" \
  --arg receiptsRoot "$(jq -r '.result.receiptsRoot' <<<"$baseline")" \
  '{result:"match", finalizedBlock:$block, hash:$hash, stateRoot:$stateRoot, receiptsRoot:$receiptsRoot}'
