# Differential campaigns

All runners are continuous: they generate traffic for a four-minute window, require useful
activity, rotate to a fresh seed, and stop on the first failure. Evidence and the replay seed are
written under `campaign-runs/`. There is no `run once` mode.

## Pinned stack

| Campaign | Nodes | Exact dependencies | Compatibility note |
| --- | --- | --- | --- |
| Ethereum | Reth/revm, Reth/EVM2, and Geth; each paired with a beacon node and eligible to propose | Reth/revm `8a993c0327d92a0e96ec37a021f5ab806026b885`; Reth/EVM2 `39b99d8a1594b3417263e67d503b4a1e908a068c`; Kurtosis Ethereum package `195fcb9093b7dd6c8d35aaf4a0f7bc956f2f1fa9` | The EVM2 ref is the pinned PR 25002 + vmTrace stack; this row does not claim it tracks current Reth main. |
| Tempo | Two signing Tempo validators on one Commonware chain; one revm and one EVM2, both eligible to propose | Tempo/revm `3fc576fe3`; Tempo/EVM2 `f27d654a4`; embedded Reth/EVM2 `39b99d8a1`; embedded EVM2 `48d26ce9`; tempo-go PR 105 `e122c6e2`; txgen `0d62b7db` | This Tempo/EVM2 ref does **not** expose `eth_getMultiProof`; the state-root campaign does not need it. Do not use it for the Zones prover. |
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
