# Tempo revm versus EVM2

This package drives two Tempo implementations on one canonical chain. The
`tempo-revm` dev node is the only block producer and transaction submission
endpoint. `tempo-evm2` has the same genesis and uses Tempo's uncertified follow
mode, backed by reth's RPC consensus importer, to independently validate and
execute every imported payload. A direct devp2p connection lets normal reth
sync backfill any blocks mined before the live RPC subscription was ready.

```sh
docker build -t tx-fuzz-tempo:local .
kurtosis run --enclave tempo-compare ./kurtosis \
  --args-file ./kurtosis/images.local.json
```

The campaign combines two input generators:

- tx-fuzz continuously rotates deterministic FuzzyVM seeds and submits random
  runtime bytecode and calldata. Block-environment opcodes are allowed because
  both engines execute the same block context.
- txgen continuously submits Tempo's maintained mixed benchmark shapes: native
  AA and expiring nonces, TIP-20 and legacy EIP-1559 transfers, MPP channel
  operations, DEX calls, and batched calls. These are structured seeds that
  complement rather than replace random bytecode generation.

The strict oracle requires each imported block, transaction order, block hash,
state root, and receipts root to match. It compares
`trace_replayBlockTransactions(block, ["vmTrace"])` for every block and samples
`trace_replayTransaction(hash, ["vmTrace"])` once per non-empty block. The
sample covers the separate transaction lookup RPC path without tracing every
transaction twice. Stock `el-forkmon` runs beside the strict oracle as a live
head/fork dashboard.

Both generators run for the configured duration and log seeds and signed raw
transactions. Those records are only failure reproducers; transactions are not
submitted independently to the candidate chain.

`images.json` records pinned CI image digests and source commits.
`images.local.json` selects native locally built images for development.
