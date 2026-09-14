# Compatibility review — 2026-09-14

The SDK dependency is the published commit from
[tempo-go #105](https://github.com/tempoxyz/tempo-go/pull/105). Its Rust oracle is
pinned at `07761a78a4ac00988533aa8acbcb6667786b625d`, independently of the newer
runtime image pins in the Kurtosis package. Oracle codec agreement must not be
presented as proof of compatibility with every current runtime behavior.

## Open tempo-go PRs reviewed

| PR | Surface not established by the original corpus | Follow-up needed |
| --- | --- | --- |
| [#78](https://github.com/tempoxyz/tempo-go/pull/78) | Typed T8 committee client helper and fork-gated integration tests; catalog coverage only proves the raw ABI exists | Typed return decoding and T7/T8+ gate tests; current helpers only recognize older explicit fork names |
| [#80](https://github.com/tempoxyz/tempo-go/pull/80) | Address/hash width validation | Defensive exact-width unit tests, including undersized fields; rejecting only oversized fields does not establish Rust parity |
| [#81](https://github.com/tempoxyz/tempo-go/pull/81) | Canonical RLP integer decoding | Defensive field-type/width/canonicality tests; valid-envelope DFF cases do not exercise rejection behavior |
| [#82](https://github.com/tempoxyz/tempo-go/pull/82) | ComputeHash's hex-versus-text input interpretation | Test prefixed/unprefixed hex and invalid input; existing DFF cases always use prefixed hex |
| [#85](https://github.com/tempoxyz/tempo-go/pull/85) | Client constructor options, nil HTTP clients, timeout option ordering | Constructor/transport tests independent of serialized transactions |
| [#87](https://github.com/tempoxyz/tempo-go/pull/87) | Nil sender/fee-payer signature scalars | Defensive SDK unit tests returning errors for incomplete typed inputs |
| [#90](https://github.com/tempoxyz/tempo-go/pull/90), [#95](https://github.com/tempoxyz/tempo-go/pull/95) | Updated geth dependencies in examples/root module | Build/test a dependency-version matrix; this module intentionally pins the SDK's existing dependency graph |
| [#92](https://github.com/tempoxyz/tempo-go/pull/92), [#98](https://github.com/tempoxyz/tempo-go/pull/98) | Release packaging and CI action updates | No additional runtime interface identified in those diffs |

This is a review of the open PR diffs, not approval, merging, or a claim their fixes
are incorporated here. In particular, the nil/decoder items above are static
review observations, not remotely exercised failure cases.

## Coverage boundaries

- Offline: all 210 ABI methods across 19 interfaces, plus 11 transaction feature
  fixtures; encoding, argument roundtrip and signature recovery tests only.
- Stateful Kurtosis scenario: 25 ordinary transactions on each independent chain;
  receipt/event equivalence, expected recipient balance, and a fixed storage
  contract's deployment and slot write. See README for cases.
- Not established: all precompile state machines, privileged administration,
  DEX matching, liquidity transitions, dynamic deployments/zones, stablecoin DEX,
  validator/committee changes, staking, fee AMM economics, account-key provisioning
  and revocation, P256/WebAuthn, separate-account sponsorship, delegated execution,
  validity/expiry behavior, reverted batches, general EVM instruction coverage,
  full state roots, reorgs, multi-validator consensus, or production fork schedules.
- Uniformly successful transactions are necessary but insufficient. The runner
  also requires matching gas/logs and an independently calculated token balance;
  it still cannot establish exhaustive protocol correctness.
