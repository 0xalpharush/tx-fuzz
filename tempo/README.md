# Tempo compatibility tools

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
transaction roundtrips and sender signature recovery where supported. The corpus
generator performs no RPC calls. It is never broadcast by the comparison tool.

For cross-language transaction encoding checks, use tempo-go's pinned Rust/DFF
suite (`make compatibility` in that repository).

## Disposable Kurtosis comparison

The separate `tempo-compare` command submits exactly 25 valid transactions per
chain: 16 seeded transfers plus batching, memo transfer, approval, access lists,
parallel nonces, explicit fee token and same-account sponsorship, followed by
deployment and execution of a fixed storage contract. It compares
transaction hashes, successful receipt status, gas used, ordered event payloads
and recipient token balances, with an independently calculated expected balance.
The storage fixture must deploy the expected bytecode and write 42 to slot zero.
Block hashes, heights, transaction/log indices and timestamps are deliberately
excluded because the two chains mine independently. It does not compare full
state roots, sender fee balances or general EVM instruction coverage.

Requires Docker, Kurtosis 1.20.0 and Go 1.25.9. From the repository root:

```sh
kurtosis run --enclave tempo-compat ./tempo/kurtosis/main.star
cd tempo
go run ./cmd/tempo-compare \
  -main-rpc "$(kurtosis port print tempo-compat main rpc)" \
  -evm2-rpc "$(kurtosis port print tempo-compat evm2 rpc)" -seed 1
cd ..
kurtosis enclave rm --force tempo-compat
```

Only literal loopback HTTP endpoints with chain ID 1337 are accepted. Use fresh
disposable enclaves: keys are public deterministic fixtures, funding uses the
localnet faucet, and reused sender accounts are rejected. No upstream spammer,
arbitrary VM generation, production RPC or funded wallet is involved.

The package pins published linux/amd64 images by digest for Tempo main
[`3fc576f`](https://github.com/tempoxyz/tempo/commit/3fc576fe3469197464b58e83d36ea6d2f49a5b45)
and klkvr/evm2
[`ac44356`](https://github.com/tempoxyz/tempo/commit/ac4435674015841bb819db0efdef13915307cfc2).
These are two independent, bootstrapped single-node dev chains, not a mixed-client
consensus network. Dev-chain activation of future forks is not equivalent to
mainnet/testnet fork scheduling. CI saves version/startup logs and JSONL results.

See [coverage review](COVERAGE.md) for intentionally unverified surfaces and the
open SDK PRs that expose missing coverage. No exhaustive compatibility claim is made.
