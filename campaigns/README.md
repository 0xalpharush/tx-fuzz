# Differential state campaigns

These runners keep generating fresh seeded campaigns until the first mismatch. A failed run keeps
its node data, logs, exact refs, seed, and generated failure artifacts under `campaign-runs/`.

## Ethereum: revm versus EVM2

```console
./campaigns/prepare-ethereum.sh
screen -dmS evm2-ethereum-state \
  bash -lc 'cd "$PWD" && exec ./campaigns/ethereum-state-loop.sh'
```

Both Reth implementations participate in one Kurtosis Ethereum chain. `tx-fuzz` and `txgen` feed
the producer; the gate compares finalized canonical blocks, including block hash, state root,
receipts root, and transaction order.

## Tempo: revm versus EVM2

```console
screen -dmS evm2-tempo-state \
  bash -lc 'cd "$PWD" && exec ./campaigns/tempo-state-loop.sh'
```

The revm node produces one Tempo chain and the EVM2 follower imports and independently executes
the same payloads. Both generators feed only the producer. The same canonical block fields are the
gate; RPC trace parity is intentionally outside this state-transition campaign.

## Tempo Zones: TIP-1098 L1 plus prover-enabled Zone

```console
./campaigns/prepare-zones.sh

screen -dmS evm2-tempo-zones \
  bash -lc 'cd "$PWD" && exec ./campaigns/tempo-zones-state-loop.sh'
```

This pins the latest-Tempo integration stack containing TIP-1096/TIP-1098 and the latest Zones
`prover` stack. The Zone sequencer runs the checker in observe mode while two independent gates run
continuously:

- `txgen-tempo-property` generates randomized deposits and withdrawals and reconstructs Portal
  backing from pinned L1 and Zone snapshots plus complete event histories.
- `tempo-zone-prover-utils` watches the canonical Zone chain for every
  `finalizeWithdrawalBatch` system transaction, independently builds the exact boundary-aligned SPF
  witness, and re-executes that range. Its computed next block hash must equal the sequencer's
  canonical block hash, which commits to the state root.

The local runner does not pretend to produce Nitro attestations. Proof-gated settlement requires an
AWS Nitro Enclave with `/dev/nsm`; the ordinary fuzz host has no NSM. SPF execution is nevertheless
enabled as the strict differential oracle, and fails the campaign on any replay mismatch.

All three runners default to a sub-five-minute feedback cycle. Each state oracle emits block and
transaction inclusion evidence as the chain advances. The Zones runner samples both heads every
five seconds, checks Portal backing, and requires at least one exact-boundary SPF replay. Completed
on-chain settlement and withdrawal lifecycles are a separate Nitro-enabled gate.

All runners accept `SEED=<u64>` for replay. Tempo Zones also accepts port overrides such as
`L1_HTTP_PORT`, `L1_WS_PORT`, `ZONE_HTTP_PORT`, and `ZONE_PRIVATE_PORT` so multiple hosts or isolated
workers can avoid collisions.
