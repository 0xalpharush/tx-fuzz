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

## D-002: `tempo-zone dev` cannot provision a Zone after TIP-1092 migration

- Status: confirmed; fix implemented; live regression running
- Found by: four-minute Tempo L1 + Zone campaign
- Input: fresh TIP-1098 Tempo dev chain, pathUSD initial token, latest Zones
  `prover` branch merged with main
- Invariant: dev provisioning must migrate the initial token's legacy TIP-403
  transfer-policy binding before calling `ZoneFactory.createZone`
- Result: `createZone` reverted with
  `TokenTransferPolicyNotSet (0x8074d401)` on every fresh run.
- Impact: `tempo-zone dev` cannot start against a current TIP-1092/TIP-1098
  Tempo chain, blocking local Zone testing and prover fuzzing.
- Root cause: the normal `create-zone` command performs
  `migrateTransferPolicyIds`, but the separate dev provisioning path omitted
  the same prerequisite and called `createZone` directly.
- Fix: dev provisioning now queries the registry, migrates the initial token
  when needed, verifies the binding, and only then anchors and creates the
  Zone.
- Regression: `cargo check -p zone-node` passes; the rebuilt live campaign is
  the end-to-end regression.

## D-003: Zone witness generation exceeds Tempo's default proof window

- Status: fixed in campaign; live regression passed
- Found by: four-minute Tempo L1 + Zone lifecycle/prover campaign
- Input: SPF witness generation for Zone blocks 1 through 16 while Tempo had
  advanced to block 86
- Invariant: every Tempo checkpoint imported by an unfinalized Zone block must
  remain available for `eth_getMultiProof` while constructing its SPF witness
- Result: `debug_zoneExecutionWitness` failed at Zone block 5 with an internal
  `eth_getMultiProof at Tempo block 11` error. Direct reproduction against the
  Tempo node returned `distance to target block exceeds maximum proof window`.
- Impact: deposits were processed and backing remained solvent, but the prover
  could not construct an input for an otherwise healthy Zone chain.
- Root cause: the test Tempo node used the default historical proof window,
  while its 500 ms block time moved the imported checkpoint outside that
  window before the first 16-block Zone batch was collected. The Zone RPC
  translated the upstream response into generic error `-32603`, hiding the
  actionable cause.
- Fix: the Zones campaign starts its archival Tempo node with a historical
  proof window large enough to cover every checkpoint produced during a run.
- Regression: the next randomized run generated all Zone and Tempo state
  witnesses in 6 ms at Tempo head 84, including the previously failing block
  11 proof.

## D-004: Tempo EVM2 state-root candidate is not a Zones L1 dependency

- Status: confirmed compatibility limitation; not a blocker for these campaigns
- Found by: direct RPC comparison on the live Tempo revm/EVM2 pair
- Engines: current Tempo revm baseline and the reth PR 25002 EVM2 candidate
- Input: identical `eth_getMultiProof` request at Tempo block 11
- Invariant: both implementations must expose the RPC methods required by Zone
  witness generation
- Result: revm handled the method and returned its configured proof-window
  error; EVM2 returned JSON-RPC `-32601 Method not found`.
- Impact: this particular Tempo/EVM2 candidate cannot be substituted for the
  TIP-1098/revm L1 used by the Zones campaign. The Zones runner does not use
  EVM2: it pins Tempo TIP-1098 with revm, settles through the genesis stub
  verifier, and checks proofs independently with the rpc-only SPF sub-verifier.
- Root cause: the EVM2 reth branch predates the RPC addition and is not yet
  rebased onto current reth main.
- Follow-up: when the Tempo EVM2 lane is refreshed onto a Reth revision that
  includes `eth_getMultiProof`, add an RPC capability regression there. This is
  independent of the Zones campaign and is not required for its state/proof
  oracle.

## H-001: counted prover wrapper selected an invalid multi-boundary batch

- Status: harness bug; removed from product findings
- Found by: Tempo L1 + Zone prover campaign after fixing D-003
- Input: `generate-input --zone-block-count 16` for Zone blocks 1 through 16;
  the first settlement boundary was block 6
- Invariant: a prover batch must end at its sole `finalizeWithdrawalBatch`
  block; finalization cannot appear in an intermediate block
- Result: witness collection succeeded, then local SPF validation returned
  `invalid batch shape` because block 6 finalized withdrawals inside the
  forced 1-through-16 range.
- Impact: no product impact. The campaign manufactured an invalid prover input
  and could have misreported it as a Zones failure.
- Root cause: counted discovery treated the requested block count as an exact
  endpoint instead of respecting the protocol's data-dependent settlement
  boundary.
- Fix: remove counted standalone input generation from the campaign's
  correctness path. The SPF oracle now discovers every canonical
  `finalizeWithdrawalBatch` system transaction and validates each exact range
  between consecutive boundaries.
- Regression: exact ranges 1..=2, 3..=95, 96..=120, and 121..=240
  passed in seed 1246255787.

## H-002: unproved dev settlement is rejected as `InvalidProof`

- Status: invalid campaign configuration; product finding not confirmed
- Found by: Zone node settlement monitor in the same D-005 run
- Input: first finalized batch, Zone blocks 1 through 6, Tempo checkpoint 11
- Invariant: a prover-gated Portal must receive the proof format required by
  its configured verifier
- Result: the node retried the batch three times; every `submitBatch` reverted
  with selector `0x09bde339` (`InvalidProof`). The Portal remained at its zero
  Zone commitment.
- Impact: no product impact established. Deposits and Zone blocks advanced, but
  the misconfigured campaign could not exercise settlement or withdrawals.
- Root cause: the campaign ran the prover branch's sequencer without a remote
  Nitro prover, so it submitted an empty/unattested proof to a proof-gated
  verifier.
- Fix: run exact-boundary SPF validation independently of settlement, retain an
  RPC-only same-chain follower for state-root comparison, and keep attested
  settlement as a separate oracle that runs only when a Nitro prover is
  configured.
- Regression: T13 rejection reproduced as expected; exact-boundary SPF
  regression is pending its first continuous campaign run.

## H-003: SPF boundary detector treated every ZoneOutbox call as finalization

- Status: harness bug; fixed
- Found by: first continuous exact-boundary SPF campaign
- Input: randomized user call to `ZoneOutbox` in Zone block 76
- Invariant: only the `finalizeWithdrawalBatch(uint256,uint64,bytes[])`
  system transaction terminates an SPF batch
- Result: the oracle incorrectly selected blocks 3 through 76, found no
  finalization in the extracted range, and SPF correctly rejected it as an
  invalid batch shape.
- Impact: no product impact. The oracle stopped on a valid user transaction and
  would have reported a false product failure.
- Root cause: boundary detection matched the ZoneOutbox destination without
  checking calldata.
- Fix: require the canonical `finalizeWithdrawalBatch` selector `0xce7025e9`.
- Regression: the corrected continuous run passed four consecutive real
  boundaries through Zone block 240.

## D-005: Zone checker rejects valid checkpoint-only blocks

- Status: confirmed; fix pending
- Found by: same-chain Zone leader and RPC-only follower in seed 1246255787
- Input: Zone block 1 containing the canonical `advanceTempoHeaders` checkpoint
  transaction
- Invariant: checkpoint-only blocks intentionally emit no `TempoAdvanced`
  event and must remain valid checker input
- Result: both independent nodes recorded `checker divergence: block 1 is
  missing TempoAdvanced` even though the payload log confirms the canonical
  checkpoint transaction and the SPF replay accepts the block.
- Impact: the observe-only solvency checker marks a valid Zone chain divergent
  at its first checkpoint-only block and stops checking all later bridge
  accounting. Zone execution and state roots remain correct.
- Root cause: the checker event collector unconditionally requires a
  `TempoAdvanced` receipt event and does not recognize the eventless
  `advanceTempoHeaders` system transaction.
- Fix: pending transaction-aware checkpoint decoding in the checker.
- Regression: pending a unit test plus a live same-chain rerun.

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
