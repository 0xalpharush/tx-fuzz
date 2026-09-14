# Tempo

This separate Go module uses tempo-go without changing the upstream Ethereum tools.

## Corpus

```sh
go run ./cmd/tempo-corpus -seed 1 -chain-id 1337 > corpus.jsonl
go test -race ./...
```

Generates deterministic transaction fixtures for the SDK's precompile ABIs and
transaction types. Tests check ABI arguments, transaction roundtrips and sender
signatures. The corpus is offline: ABI-valid arguments do not imply an executable
state transition, and dynamic contracts use placeholder addresses.

## Localnet comparison

Requires Docker and Kurtosis 1.20.0. From this directory:

```sh
kurtosis run --enclave tempo-compare ./kurtosis/main.star --args-file ./kurtosis/images.json
go run ./cmd/tempo-compare \
  -baseline-rpc "$(kurtosis port print tempo-compare baseline rpc)" \
  -candidate-rpc "$(kurtosis port print tempo-compare candidate rpc)" -seed 1
kurtosis enclave rm --force tempo-compare
```

Set `baseline_image` and `candidate_image` in `kurtosis/images.json` to the
linux/amd64 localnet image digests being compared. Each service runs an independent
dev chain. Use fresh enclaves: the runner accepts only loopback HTTP endpoints on
chain ID 1337 and funds public fixture keys through the localnet faucet.

The runner exercises transfers, batching, memos, approvals, access lists, parallel
nonces, fee-token selection, same-account sponsorship, and a fixed storage contract.
It compares hashes, receipt status/gas/events, expected recipient balances, deployed
code and storage. Block metadata is excluded because the chains mine independently.
This is a bounded regression suite, not full protocol or consensus coverage.
