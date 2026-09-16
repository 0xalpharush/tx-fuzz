// tempo-fund pre-funds deterministic workload accounts through Tempo's dev
// faucet and waits until every mint is canonical before generators start.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
)

type receipt struct {
	BlockHash common.Hash `json:"blockHash"`
}

func waitReceipt(ctx context.Context, client *rpc.Client, hash common.Hash) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var got *receipt
		if err := client.CallContext(ctx, &got, "eth_getTransactionReceipt", hash); err != nil {
			return err
		}
		if got != nil && got.BlockHash != (common.Hash{}) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func run(ctx context.Context, endpoint string, addresses []string, out *json.Encoder) error {
	client, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		return err
	}
	defer client.Close()

	for _, raw := range addresses {
		raw = strings.TrimSpace(raw)
		if !common.IsHexAddress(raw) {
			return fmt.Errorf("invalid funding address %q", raw)
		}
		address := common.HexToAddress(raw)
		var hashes []common.Hash
		if err := client.CallContext(ctx, &hashes, "tempo_fundAddress", address); err != nil {
			return fmt.Errorf("fund %s: %w", address, err)
		}
		if len(hashes) == 0 {
			return fmt.Errorf("fund %s: faucet returned no transactions", address)
		}
		for _, hash := range hashes {
			if err := waitReceipt(ctx, client, hash); err != nil {
				return fmt.Errorf("fund %s transaction %s: %w", address, hash, err)
			}
		}
		if err := out.Encode(map[string]any{"event": "funded", "address": address, "transactions": hashes}); err != nil {
			return err
		}
	}
	return out.Encode(map[string]any{"event": "ready", "fundedAccounts": len(addresses)})
}

func main() {
	endpoint := flag.String("rpc", "http://127.0.0.1:8545", "Tempo RPC endpoint")
	addresses := flag.String("addresses", "", "comma-separated addresses to pre-fund")
	timeout := flag.Duration("timeout", 2*time.Minute, "funding deadline")
	flag.Parse()
	if strings.TrimSpace(*addresses) == "" {
		fmt.Fprintln(os.Stderr, "--addresses is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := run(ctx, *endpoint, strings.Split(*addresses, ","), json.NewEncoder(os.Stdout)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
