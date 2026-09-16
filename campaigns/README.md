# Differential campaigns

All runners are continuous: they generate traffic for a four-minute window, require useful
activity, rotate to a fresh seed, and stop on the first failure. Evidence and the replay seed are
written under `campaign-runs/`. There is no `run once` mode.

## Pinned stack

| Campaign | Nodes | Exact dependencies | Compatibility note |
| --- | --- | --- | --- |
| Ethereum | Reth/revm, Reth/EVM2, and Geth; each paired with a beacon node and eligible to propose | Reth/revm `843d459f34f2df17d8c082bd24b70461f9c39475`; Reth/EVM2 `54228809870bd17d99fa9d32d979020555b8a28c`; Ethereum package `c0db06b29b8266e65c9b80b64895e07058d28d0b`; Geth/Lighthouse images are digest-pinned in `ethereum/network_params.yaml` | The package patch selects its enode helper. The EVM2 head descends from requested reth/revm bump `da1377865aea52959417401380ec34ed8e0d930f` and adds only the current production-replay/fuzz hooks. |
| Tempo | Two signing Tempo validators on one Commonware chain; one revm and one EVM2, both eligible to propose | Tempo/revm `ca57dfdc9e808aea6c48f07978be156cb81a2b7b`; Tempo/EVM2 `dffd26048cbeb638bed66c5371e9f254845bd1a1`; embedded reth `54228809870bd17d99fa9d32d979020555b8a28c`; embedded EVM2 `7bbf1d0dd10077a6b0b1bcfb02bb93249afca9fb`; txgen `0d62b7db` | The canonical head restores Tempo's intentional failed-transaction fee receipt and compiled genesis. The candidate contains the confirmed nonce-zero calldata-floor fix. |
| Zones | TIP-1098 Tempo L1, a Zone leader, and an independent rpc-only Zone sub-verifier | Tempo/TIP-1098 `15b79e0c63d0ac98f5b85cae2ba858155b5301d3` (Reth `95823365b9f0787a676de38c044b54830e3fb29d`); Zones `1aaabe19b10703c42324ccce4c19a7336f227b21`; txgen `0d62b7db` | L1 uses the pre-T13 stub verifier for settlement. The rpc-only sub-verifier runs the SPF in process and must independently accept canonical batches; no Nitro attestation is required. |

## Run

From the `tx-fuzz` checkout, prepare the images once:

```console
./campaigns/prepare-ethereum.sh
./campaigns/prepare-tempo.sh
./campaigns/prepare-zones.sh
```

Then start all three campaigns:

```console
screen -dmS evm2-ethereum bash -lc 'cd "'"$PWD"'" && exec ./campaigns/ethereum-state-loop.sh'
screen -dmS evm2-tempo bash -lc 'cd "'"$PWD"'" && exec ./campaigns/tempo-state-loop.sh'
screen -dmS evm2-zones bash -lc 'cd "'"$PWD"'" && exec ./campaigns/tempo-zones-state-loop.sh'
```

Use `screen -ls` to list them and `screen -r evm2-tempo` (or the other name) to watch one.

## Gates

- Ethereum and Tempo fail semantically only when revm and EVM2 disagree on the state root at the
  same canonical height. Empty-traffic windows, stalled chains, or a node that never proposes are
  invalid campaigns and fail as health errors.
- Both use `tx-fuzz` for randomized EVM programs and `txgen` for structured transactions. Tempo's
  tx-fuzz stream deliberately mixes Ethereum legacy/dynamic-fee envelopes with Tempo plain,
  batched TIP-20, parallel-nonce, fee-token, sponsored, and authorization envelopes.
- Zones randomizes deposit and withdrawal lifecycles, checks L1/Zone progress and Portal backing,
  settles through the always-true stub verifier, and requires the independent in-process SPF
  verifier to validate a finalized batch.

Set `SEED=<u64>` to replay a failure. The runner continues replaying that seed until stopped.
