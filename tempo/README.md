# Offline Tempo corpus

```sh
cd tempo
go run ./cmd/tempo-corpus -seed 1 -chain-id 42431 > corpus.jsonl
go test -race ./...
```

This isolated Go module uses tempo-go and leaves the upstream Ethereum tools
unchanged. It emits 221 reproducible JSONL cases: one for every function across
the SDK's 19 Rust-derived precompile interfaces (210 total), plus 11 transaction
features: batching, creation, access lists, parallel/expiring nonces, validity
windows, fee tokens, sponsorship, delegation, keychain and inline key authorization.

Every case contains the seed, coverage label and serialized transaction. The
signing keys are public deterministic fixtures; never fund those accounts.
Targets without fixed system addresses use a fixture address. Generated
arguments are ABI-valid samples, not an executable stateful scenario. Reads,
writes, privileged operations and hardfork-specific functions all appear.

Tests verify reproducibility, method coverage, argument decoding/re-encoding,
transaction roundtrips and sender signature recovery where supported. No RPC,
network transaction sending, or live spam mode is added. State transitions,
permissions, token liquidity, P256/WebAuthn signing and exhaustive protocol
behavior remain outside this corpus's verified coverage.

For cross-language transaction encoding checks, use tempo-go's pinned Rust/DFF
suite (`make compatibility` in that repository).
