# Differential findings ledger

This ledger records confirmed revm/EVM2 differences found by the continuous
Ethereum and Tempo campaigns. A finding remains open until its fix passes the
original reproducer and a fresh randomized soak.

## D-001: Tempo precompile marker leaks into single-transaction VM trace

- Status: confirmed; minimal fix implemented; differential regression pending
- Found by: Tempo same-chain RPC oracle
- Engines: Tempo revm `3fc576fe3`; Tempo EVM2 `11dddd681` using reth
  `4020f1ab`
- Input: mined Tempo AA transaction (`0x76`) calling the TIP-20 precompile
- Invariant: block and individual replay of the same canonical transaction must
  produce equivalent `vmTrace` output on both engines
- Result: block hashes, state roots, receipt roots, and block replay agreed.
  `trace_replayTransaction` returned root `vmTrace.code = 0x` on revm and
  `vmTrace.code = 0xef` on EVM2.
- Impact: RPC clients that reconstruct execution from opcode traces can observe
  engine-dependent bytecode for a precompile call. Comparing only block replay
  would miss the incompatibility.
- Root cause: EVM2 state-aware Parity trace construction loads the TIP-20
  account marker byte (`0xef`) as executable root bytecode even though the call
  was handled as a precompile and executed no EVM bytecode.
- Related: reth PR 27213 addresses the adjacent generic bug that block replay
  does not populate VM bytecode. EVM2 PR 428 fixes VM execution deltas and
  nested subtrace attribution. Neither suppresses non-executed root precompile
  marker code; the upstream version of this fix should stack on PR 428.
- Reproducer: `0xalpharush/tx-fuzz` PR 2. The smoke run used seed `917431`, but
  the first funded TIP-20 transaction is sufficient and seed-independent.
- Fix: `0xalpharush/evm2@1246a6b3` marks the root call as a precompile for
  state-aware bytecode population and suppresses only nested zero-value
  precompile flat traces. The historical branch is based on the original
  `a614d8e0` reproducer baseline; the upstream submission should stack on EVM2
  PR 428.
- Regression: EVM2 inspector integration suite passed (34 tests), including
  all 8 Parity trace tests and the new root-precompile marker test. The original
  same-chain Tempo reproducer still needs to pass on the combined upstream
  stack, followed by a fresh randomized soak.

## Campaign baseline migration: 2026-09-14

- reth: `0xalpharush/reth@6c703894`, branch
  `evm2/pr-25002-latest-evm2`; this is reth PR 25002 forward-ported from EVM2
  `a614d8e0` to latest EVM2 main `07f6c749`.
- Tempo: `0xalpharush/tempo@cf29b4a0c`, same branch name, based on Tempo
  `11dddd681` and pinned to the reth forward-port and EVM2 `07f6c749`.
- Compatibility fixes: native-precompile events are buffered during Tempo's
  exclusive state borrow, checkpoint-aware, and then forwarded through
  `Evm::log` so inspectors receive each event exactly once; Tempo also accepts
  the new EVM2 floor-gas hook without changing its gas schedule.
- Validation: reth targeted checks, test builds, 51 executed tests, and format
  checks passed. Tempo node/EVM checks passed; the precompile log/checkpoint
  regression passed; all 101 Tempo handler tests passed. The exact Tempo
  candidate Docker build compiled and exported successfully.
- Next: rerun the same-chain revm/EVM2 oracle with tx-fuzz, txgen, and
  el-forkmon, then stack D-001 on EVM2 PR 428 and repeat. Local execution is
  temporarily blocked by Docker containerd metadata/blob I/O corruption after
  the host disk filled; repairing it requires either a Docker data reset or a
  clean runner.

## Entry template

- Status:
- Found by:
- Engines:
- Input:
- Invariant:
- Result:
- Impact:
- Root cause:
- Related:
- Reproducer:
- Fix:
- Regression:
