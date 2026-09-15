#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_root=${WORK_ROOT:-"$(dirname "$repo_root")"}
zones_repo=${ZONES_REPO:-"$work_root/zones"}
tempo_repo=${TEMPO_TIP1098_REPO:-"$work_root/tempo-tip1098"}
txgen_repo=${TXGEN_REPO:-"$work_root/txgen"}
run_root=${RUN_ROOT:-"$repo_root/campaign-runs/tempo-zones"}
duration=${DURATION:-4m}
l1_http_port=${L1_HTTP_PORT:-28545}
l1_ws_port=${L1_WS_PORT:-28546}
zone_http_port=${ZONE_HTTP_PORT:-19545}
zone_private_port=${ZONE_PRIVATE_PORT:-18544}
shadow_http_port=${SHADOW_HTTP_PORT:-19555}
leader_p2p_port=${LEADER_P2P_PORT:-19200}
shadow_p2p_port=${SHADOW_P2P_PORT:-19201}
property_steps=${PROPERTY_MAX_STEPS:-50}
property_verify_interval=${PROPERTY_VERIFY_INTERVAL:-10}
dev_key=${DEV_KEY:-0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}
dev_address=0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266
token=${ZONE_TOKEN:-0x20C0000000000000000000000000000000000000}
genesis_template=${GENESIS_TEMPLATE:-"$repo_root/tempo/kurtosis/dev-genesis/dev.json"}
mkdir -p "$run_root"

cleanup_run() {
  docker stop "$tempo_container" "$zone_container" "$shadow_container" "$property_container" "$oracle_container" "${spf_container:-}" >/dev/null 2>&1 || true
  for pid in "${timer_pid:-}" "${tempo_pid:-}" "${zone_pid:-}" "${shadow_pid:-}" "${property_pid:-}" "${oracle_pid:-}" "${spf_pid:-}"; do
    if [[ -n "$pid" ]]; then kill "$pid" >/dev/null 2>&1 || true; fi
  done
}

run_spf_oracle() {
  local next_block=1
  local previous_boundary=0
  local head_hex head block_hex request finalizations from_block

  while true; do
    request=$(jq -nc '{jsonrpc:"2.0",id:1,method:"eth_blockNumber",params:[]}')
    head_hex=$(curl --fail --silent --show-error --header 'content-type: application/json' \
      --data "$request" "$zone_rpc" | jq -er .result)
    head=$((head_hex))

    while (( next_block <= head )); do
      block_hex=$(printf '0x%x' "$next_block")
      request=$(jq -nc --arg block "$block_hex" \
        '{jsonrpc:"2.0",id:1,method:"eth_getBlockByNumber",params:[$block,true]}')
      finalizations=$(curl --fail --silent --show-error --header 'content-type: application/json' \
        --data "$request" "$zone_rpc" | jq -er \
        '[.result.transactions[] | select(
          (.to // "" | ascii_downcase) == "0x1c00000000000000000000000000000000000002"
          and (.input // "" | startswith("0xce7025e9"))
        )] | length')
      if (( finalizations > 0 )); then
        from_block=$((previous_boundary + 1))
        docker run --rm --network host --name "$spf_container" --user "$(id -u):$(id -g)" \
          --volume "$evidence/zone-config:/config:ro" \
          tempo-zone-prover-utils:prover-latest generate-input \
          --tempo-rpc-url "$l1_rpc" --chain /config/genesis.json \
          --zone-private-rpc-url "$zone_private_rpc" \
          --zone-unrestricted-rpc-url "$zone_rpc" --private-key "$dev_key" \
          --from-block "$from_block" --to-block "$next_block"
        echo "SPF_VERIFIED from=$from_block to=$next_block"
        previous_boundary=$next_block
      fi
      next_block=$((next_block + 1))
    done
    sleep 1
  done
}

while true; do
  started=$(date -u +%Y%m%dT%H%M%SZ)
  started_lower=$(printf '%s' "$started" | tr '[:upper:]' '[:lower:]')
  seed=${SEED:-$((10#$(date -u +%s) ^ RANDOM << 15 ^ RANDOM))}
  run="tempo-zones-${started_lower}-${seed}"
  evidence="$run_root/$run"
  tempo_container="${run}-tempo"
  zone_container="${run}-zone"
  shadow_container="${run}-shadow"
  property_container="${run}-property"
  oracle_container="${run}-oracle"
  spf_container="${run}-spf"
  mkdir -p "$evidence/tempo-data" "$evidence/zone-config" "$evidence/leader-data" \
    "$evidence/shadow-data" "$evidence/property-failures"
  jq --arg timestamp "$(printf '0x%x' "$(( $(date -u +%s) - 2 ))")" \
    --arg verifierCode '0x600160005260206000f3' \
    '.timestamp = $timestamp
      | .config.t13Time = 9999999999999
      | .alloc["0x5a56000000000000000000000000000000000000"].code = $verifierCode' \
    "$genesis_template" >"$evidence/dev.json"

  jq -n \
    --arg event started --arg chain tempo-zones --arg run "$run" --arg started "$started" \
    --argjson seed "$seed" --arg duration "$duration" \
    --arg zonesRef "$(git -C "$zones_repo" rev-parse HEAD)" \
    --arg tempoRef "$(git -C "$tempo_repo" rev-parse HEAD)" \
    --arg txgenRef "$(git -C "$txgen_repo" rev-parse HEAD)" \
    '{event:$event,chain:$chain,run:$run,started:$started,seed:$seed,duration:$duration,zonesRef:$zonesRef,tempoRef:$tempoRef,txgenRef:$txgenRef}' \
    | tee "$evidence/status.json"

  trap cleanup_run EXIT INT TERM
  docker run --rm --network host \
    --name "$tempo_container" \
    --volume "$evidence/dev.json:/config/dev.json:ro" \
    --volume "$evidence/tempo-data:/data" \
    tempo-tip1098:local \
    node --dev --addr 0.0.0.0 --chain /config/dev.json --dev.block-time 500ms \
    --datadir /data --tempo.bootnodes-endpoint none --disable-discovery --no-persist-peers \
    --rpc.eth-proof-window 100000 \
    --http --http.addr 0.0.0.0 --http.port "$l1_http_port" --http.api all \
    --ws --ws.addr 0.0.0.0 --ws.port "$l1_ws_port" --ws.api all \
    --builder.gaslimit 3000000000 \
    --faucet.enabled --faucet.private-key "${dev_key#0x}" \
    --faucet.amount 1000000000000000 \
    --faucet.node-address "http://127.0.0.1:$l1_http_port" \
    --faucet.address 0x20c0000000000000000000000000000000000000 \
    0x20c0000000000000000000000000000000000001 \
    0x20c0000000000000000000000000000000000002 \
    0x20c0000000000000000000000000000000000003 \
    >"$evidence/tempo.log" 2>&1 &
  tempo_pid=$!

  l1_rpc="http://127.0.0.1:$l1_http_port"
  l1_ws="ws://127.0.0.1:$l1_ws_port"
  result=0
  for _ in $(seq 1 180); do
    if ! kill -0 "$tempo_pid" 2>/dev/null; then result=1; break; fi
    if curl --fail --silent --show-error \
      --header 'content-type: application/json' \
      --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' \
      "$l1_rpc" >/dev/null; then
      break
    fi
    sleep 2
  done

  if (( result == 0 )); then
    leader_public=$(docker run --rm --user "$(id -u):$(id -g)" \
      --volume "$evidence/zone-config:/config" tempo-zone-xtask:prover-latest \
      generate-p2p-key --out /config/leader-p2p.key)
    follower_a_public=$(docker run --rm --user "$(id -u):$(id -g)" \
      --volume "$evidence/zone-config:/config" tempo-zone-xtask:prover-latest \
      generate-p2p-key --out /config/follower-a-p2p.key)
    follower_b_public=$(docker run --rm --user "$(id -u):$(id -g)" \
      --volume "$evidence/zone-config:/config" tempo-zone-xtask:prover-latest \
      generate-p2p-key --out /config/follower-b-p2p.key)
    shadow_public=$(docker run --rm --user "$(id -u):$(id -g)" \
      --volume "$evidence/zone-config:/config" tempo-zone-xtask:prover-latest \
      generate-p2p-key --out /config/shadow-p2p.key)
    umask 077
    printf '%s\n' "$dev_key" >"$evidence/zone-config/leader-secp256k1.key"
    printf '%s\n' "$dev_key" >"$evidence/zone-config/sequencer.key"

    docker run --rm --network host --user "$(id -u):$(id -g)" \
      --env "ZONE_FACTORY_OWNER_KEY=$dev_key" \
      --volume "$evidence/zone-config:/output" tempo-zone-xtask:prover-latest \
      create-zone --output /output --l1-rpc-url "$l1_rpc" --initial-token "$token" \
      --sequencer "$dev_address" --sequencer 0x1111111111111111111111111111111111111111 \
      --sequencer 0x2222222222222222222222222222222222222222 --threshold 1 \
      --admin "$dev_address" --rpc-url "http://127.0.0.1:$zone_http_port" \
      >"$evidence/provision.log" 2>&1 || result=$?
    if (( result == 0 )); then
      portal=$(jq -er .portal "$evidence/zone-config/zone.json")
      docker run --rm --network host --user "$(id -u):$(id -g)" \
        --env "PRIVATE_KEY=$dev_key" tempo-zone-xtask:prover-latest \
        set-encryption-key --l1-rpc-url "$l1_rpc" --portal "$portal" \
        >>"$evidence/provision.log" 2>&1 || result=$?
    fi
  fi

  if (( result == 0 )); then
    portal=$(jq -er .portal "$evidence/zone-config/zone.json")
    expected_stub=$(jq -er '.alloc["0x5a56000000000000000000000000000000000000"].code' \
      "$evidence/dev.json")
    request=$(jq -nc \
      '{jsonrpc:"2.0",id:1,method:"eth_getCode",params:["0x5a56000000000000000000000000000000000000","latest"]}')
    actual_stub=$(curl --fail --silent --show-error --header 'content-type: application/json' \
      --data "$request" "$l1_rpc" | jq -er .result)
    actual_stub_lower=$(printf '%s' "$actual_stub" | tr '[:upper:]' '[:lower:]')
    expected_stub_lower=$(printf '%s' "$expected_stub" | tr '[:upper:]' '[:lower:]')
    if [[ "$actual_stub_lower" != "$expected_stub_lower" ]]; then
      echo "Tempo L1 does not have the pinned always-true stub verifier" >&2
      result=1
    fi
    request=$(jq -nc --arg to "$portal" \
      '{jsonrpc:"2.0",id:1,method:"eth_call",params:[{to:$to,data:"0x2b7ac3f3"},"latest"]}')
    portal_verifier=$(curl --fail --silent --show-error --header 'content-type: application/json' \
      --data "$request" "$l1_rpc" | jq -er .result)
    if [[ "${portal_verifier: -40}" != "5a56000000000000000000000000000000000000" ]]; then
      echo "Zone Portal does not point at the pinned stub verifier" >&2
      result=1
    fi
  fi

  if (( result == 0 )); then
    portal=$(jq -er .portal "$evidence/zone-config/zone.json")
    cat >"$evidence/zone-config/manifest.toml" <<EOF
leader_ed25519_public_key = "$leader_public"

[[nodes]]
name = "leader"
ed25519_public_key = "$leader_public"
secp256k1_address = "$dev_address"
address = "127.0.0.1:$leader_p2p_port"

[[nodes]]
name = "follower-a"
ed25519_public_key = "$follower_a_public"
secp256k1_address = "0x1111111111111111111111111111111111111111"
address = "127.0.0.1:19202"

[[nodes]]
name = "follower-b"
ed25519_public_key = "$follower_b_public"
secp256k1_address = "0x2222222222222222222222222222222222222222"
address = "127.0.0.1:19203"

[[nodes]]
name = "shadow"
ed25519_public_key = "$shadow_public"
address = "127.0.0.1:$shadow_p2p_port"
rpc_only = true
EOF

    docker run --rm --network host --name "$zone_container" --user "$(id -u):$(id -g)" \
      --volume "$evidence/zone-config:/config:ro" --volume "$evidence/leader-data:/data" \
      tempo-zone:prover-latest node --chain /config/genesis.json --l1.rpc-url "$l1_ws" \
      --l1.portal-address "$portal" --datadir /data --log.file.directory /data/logs \
      --http --http.addr 0.0.0.0 \
      --http.port "$zone_http_port" --http.api all --ws --ws.addr 0.0.0.0 \
      --ws.port "$((zone_http_port + 1))" --ws.api all --redacted-rpc.port "$zone_private_port" \
      --sequencer.manifest /config/manifest.toml --p2p.key /config/leader-p2p.key \
      --secp256k1.key /config/leader-secp256k1.key --p2p.listen "0.0.0.0:$leader_p2p_port" \
      --sequencer.role leader --sequencer-key-file /config/sequencer.key --checker.mode observe \
      >"$evidence/zone.log" 2>&1 &
    zone_pid=$!

    docker run --rm --network host --name "$shadow_container" --user "$(id -u):$(id -g)" \
      --volume "$evidence/zone-config:/config:ro" --volume "$evidence/shadow-data:/data" \
      tempo-zone:prover-latest node --chain /config/genesis.json --l1.rpc-url "$l1_ws" \
      --l1.portal-address "$portal" --datadir /data --log.file.directory /data/logs \
      --http --http.addr 0.0.0.0 \
      --http.port "$shadow_http_port" --http.api all --ws --ws.addr 0.0.0.0 \
      --ws.port "$((shadow_http_port + 1))" --ws.api all --redacted-rpc.port "$((zone_private_port + 10))" \
      --sequencer.manifest /config/manifest.toml --p2p.key /config/shadow-p2p.key \
      --p2p.listen "0.0.0.0:$shadow_p2p_port" --sequencer.role rpc-follower \
      --sequencer.enable-prover --checker.mode observe >"$evidence/shadow.log" 2>&1 &
    shadow_pid=$!
  fi

  zone_rpc="http://127.0.0.1:$zone_http_port"
  shadow_rpc="http://127.0.0.1:$shadow_http_port"
  zone_private_rpc="http://127.0.0.1:$zone_private_port"
  if (( result == 0 )); then
    for _ in $(seq 1 180); do
      if ! kill -0 "$zone_pid" 2>/dev/null || ! kill -0 "$shadow_pid" 2>/dev/null; then result=1; break; fi
      if curl --fail --silent --show-error \
        --header 'content-type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' \
        "$zone_rpc" >/dev/null && curl --fail --silent --show-error \
        --header 'content-type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' \
        "$shadow_rpc" >/dev/null; then
        break
      fi
      sleep 2
    done
  fi

  if (( result == 0 )) && [[ -s "$evidence/zone-config/zone.json" ]]; then
    run_spf_oracle >"$evidence/spf-oracle.log" 2>&1 &
    spf_pid=$!

    docker run --rm --network host --name "$oracle_container" \
      --entrypoint /usr/local/bin/zone-lifecycle-oracle tx-fuzz-tempo:local \
      --l1-rpc "$l1_rpc" --zone-rpc "$zone_rpc" --shadow-zone-rpc "$shadow_rpc" \
      --portal "$portal" --duration "$duration" \
      >"$evidence/lifecycle-oracle.jsonl" 2>&1 &
    oracle_pid=$!

    docker run --rm --network host \
      --name "$property_container" --env "PRIVATE_KEY=$dev_key" \
      --volume "$evidence/zone-config/zone.json:/zone.json:ro" \
      --volume "$evidence/property-failures:/property-failures" \
      --entrypoint /usr/local/bin/txgen-tempo-property txgen-differential:local \
      --zone-config /zone.json --l1-rpc-url "$l1_rpc" \
      --zone-rpc-url "$zone_rpc" --zone-private-rpc-url "$zone_private_rpc" \
      --token "$token" --continuous --max-steps "$property_steps" --seed "$seed" \
      --verify-every-steps "$property_verify_interval" \
      --failure-directory /property-failures >"$evidence/property.log" 2>&1 &
    property_pid=$!

    while kill -0 "$oracle_pid" 2>/dev/null; do
      for process in tempo_pid zone_pid shadow_pid property_pid spf_pid; do
        pid=${!process}
        if ! kill -0 "$pid" 2>/dev/null; then
          wait "$pid" || result=$?
          if (( result == 0 )); then result=1; fi
          break 2
        fi
      done
      sleep 5
    done
    if (( result == 0 )); then
      wait "$oracle_pid" || result=$?
    fi
    if (( result == 0 )) && ! grep -q '^SPF_VERIFIED ' "$evidence/spf-oracle.log"; then
      echo "in-process SPF oracle validated no proposed batch" >&2
      result=1
    fi
  else
    result=1
  fi

  cleanup_run
  for pid in "${tempo_pid:-}" "${zone_pid:-}" "${shadow_pid:-}" "${property_pid:-}" "${oracle_pid:-}" "${spf_pid:-}"; do
    if [[ -n "$pid" ]]; then wait "$pid" 2>/dev/null || true; fi
  done
  finished=$(date -u +%Y%m%dT%H%M%SZ)

  if (( result != 0 )); then
    jq -n --arg event failed --arg chain tempo-zones --arg run "$run" \
      --arg finished "$finished" --argjson seed "$seed" --argjson exitCode "$result" \
      '{event:$event,chain:$chain,run:$run,finished:$finished,seed:$seed,exitCode:$exitCode}' \
      | tee "$evidence/status.json"
    trap - EXIT INT TERM
    exit "$result"
  fi

  jq -n --arg event passed --arg chain tempo-zones --arg run "$run" \
    --arg finished "$finished" --argjson seed "$seed" \
    '{event:$event,chain:$chain,run:$run,finished:$finished,seed:$seed}' \
    | tee "$evidence/status.json"
  trap - EXIT INT TERM
  unset SEED tempo_pid zone_pid shadow_pid property_pid oracle_pid spf_pid timer_pid
done
