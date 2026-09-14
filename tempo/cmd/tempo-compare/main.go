// tempo-compare runs a bounded set of ordinary transactions on two disposable
// local chains. It never runs the upstream spammer or the offline ABI corpus.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"math/rand"
	"net"
	"net/url"
	"os"
	"reflect"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/tempoxyz/tempo-go/pkg/precompiles"
	"github.com/tempoxyz/tempo-go/pkg/signer"
	"github.com/tempoxyz/tempo-go/pkg/transaction"
)

type logEntry struct {
	Address common.Address `json:"address"`
	Topics  []common.Hash  `json:"topics"`
	Data    hexutil.Bytes  `json:"data"`
}
type receipt struct {
	Status  hexutil.Uint64 `json:"status"`
	GasUsed hexutil.Uint64 `json:"gasUsed"`
	Logs    []logEntry     `json:"logs"`
}
type result struct {
	Case             string      `json:"case"`
	Hash             common.Hash `json:"hash"`
	Receipt          *receipt    `json:"receipt"`
	RecipientBalance string      `json:"recipientBalance"`
}

func localEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.User != nil {
		return fmt.Errorf("endpoint must be a literal loopback HTTP address: %q", raw)
	}
	return nil
}

func waitReceipt(ctx context.Context, c *rpc.Client, hash common.Hash) (*receipt, error) {
	timer := time.NewTicker(200 * time.Millisecond)
	defer timer.Stop()
	for {
		var r *receipt
		if err := c.CallContext(ctx, &r, "eth_getTransactionReceipt", hash); err != nil {
			return nil, err
		}
		if r != nil {
			return r, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func run(ctx context.Context, endpoints [2]string, seed int64, out *json.Encoder) error {
	var clients [2]*rpc.Client
	for i, endpoint := range endpoints {
		if err := localEndpoint(endpoint); err != nil {
			return err
		}
		c, err := rpc.DialContext(ctx, endpoint)
		if err != nil {
			return err
		}
		defer c.Close()
		clients[i] = c
		var chain hexutil.Uint64
		if err := c.CallContext(ctx, &chain, "eth_chainId"); err != nil {
			return err
		}
		if chain != 1337 {
			return fmt.Errorf("expected disposable localnet chain 1337, got %d", chain)
		}
	}
	key, err := crypto.ToECDSA(crypto.Keccak256([]byte(fmt.Sprintf("tx-fuzz localnet fixture %d", seed))))
	if err != nil {
		return err
	}
	s := signer.NewSignerFromKey(key)
	recipient := common.HexToAddress("0x1111111111111111111111111111111111111111")
	for _, c := range clients {
		var nonce hexutil.Uint64
		if err := c.CallContext(ctx, &nonce, "eth_getTransactionCount", s.Address(), "latest"); err != nil {
			return err
		}
		if nonce != 0 {
			return fmt.Errorf("fixture account already used; create fresh enclaves")
		}
		var hashes []common.Hash
		if err := c.CallContext(ctx, &hashes, "tempo_fundAddress", s.Address()); err != nil {
			return err
		}
		if len(hashes) == 0 {
			return fmt.Errorf("faucet returned no transactions")
		}
		for _, hash := range hashes {
			r, err := waitReceipt(ctx, c, hash)
			if err != nil {
				return err
			}
			if r.Status != 1 {
				return fmt.Errorf("faucet reverted")
			}
		}
	}
	rng := rand.New(rand.NewSource(seed))
	var nonce uint64
	var total big.Int
	names := []string{"batch", "memo", "approve", "access-list", "parallel-nonce", "fee-token", "sponsored"}
	for i := 0; i < 16; i++ {
		names = append(names, fmt.Sprintf("transfer-%02d", i))
	}
	for _, name := range names {
		amount := big.NewInt(1 + rng.Int63n(1000))
		call, err := precompiles.Call("ITIP20", transaction.AlphaUSDAddress, "transfer", recipient, amount)
		if err != nil {
			return err
		}
		tx := transaction.NewDefault(1337)
		tx.Nonce = nonce
		tx.Gas = 1000000
		tx.MaxFeePerGas.SetUint64(20000000000)
		tx.MaxPriorityFeePerGas.Set(tx.MaxFeePerGas)
		tx.Calls = []transaction.Call{call}
		expected := new(big.Int).Set(amount)
		switch name {
		case "batch":
			tx.Calls = append(tx.Calls, call)
			expected.Mul(expected, big.NewInt(2))
		case "memo":
			var memo [32]byte
			copy(memo[:], "tempo compatibility")
			tx.Calls[0], err = precompiles.Call("ITIP20", transaction.AlphaUSDAddress, "transferWithMemo", recipient, amount, memo)
		case "approve":
			tx.Calls[0], err = precompiles.Call("ITIP20", transaction.AlphaUSDAddress, "approve", recipient, amount)
			expected.SetInt64(0)
		case "access-list":
			tx.AccessList = transaction.AccessList{{Address: transaction.AlphaUSDAddress, StorageKeys: []common.Hash{{}}}}
		case "parallel-nonce":
			tx.NonceKey.SetUint64(42)
			tx.Nonce = 0
		case "fee-token":
			tx.FeeToken = transaction.AlphaUSDAddress
		case "sponsored":
			tx.AwaitingFeePayer = true
		}
		if err != nil {
			return err
		}
		if err := transaction.SignTransaction(tx, s); err != nil {
			return err
		}
		if name == "sponsored" {
			if err := transaction.AddFeePayerSignature(tx, s); err != nil {
				return err
			}
		}
		raw, err := transaction.Serialize(tx, nil)
		if err != nil {
			return err
		}
		total.Add(&total, expected)
		var results [2]result
		for i, c := range clients {
			var hash common.Hash
			if err := c.CallContext(ctx, &hash, "eth_sendRawTransaction", raw); err != nil {
				return fmt.Errorf("%s chain %d submission: %w", name, i, err)
			}
			r, err := waitReceipt(ctx, c, hash)
			if err != nil {
				return err
			}
			if r.Status != 1 {
				return fmt.Errorf("%s chain %d reverted", name, i)
			}
			balanceCall, err := precompiles.Call("ITIP20", transaction.AlphaUSDAddress, "balanceOf", recipient)
			if err != nil {
				return err
			}
			var balance hexutil.Bytes
			if err := c.CallContext(ctx, &balance, "eth_call", map[string]any{"to": transaction.AlphaUSDAddress, "data": hexutil.Encode(balanceCall.Data)}, "latest"); err != nil {
				return err
			}
			if new(big.Int).SetBytes(balance).Cmp(&total) != 0 {
				return fmt.Errorf("%s chain %d recipient balance: got %s want %s", name, i, new(big.Int).SetBytes(balance), &total)
			}
			results[i] = result{Case: name, Hash: hash, Receipt: r, RecipientBalance: hexutil.Encode(balance)}
		}
		if !reflect.DeepEqual(results[0], results[1]) {
			_ = out.Encode(map[string]any{"mismatch": name, "main": results[0], "evm2": results[1]})
			return fmt.Errorf("%s receipt/state mismatch", name)
		}
		if err := out.Encode(results[0]); err != nil {
			return err
		}
		if name != "parallel-nonce" {
			nonce++
		}
	}
	return out.Encode(map[string]any{"passed": len(names), "seed": seed, "chainId": 1337})
}

func main() {
	mainRPC := flag.String("main-rpc", "", "fresh main localnet RPC (loopback only)")
	evmRPC := flag.String("evm2-rpc", "", "fresh evm2 localnet RPC (loopback only)")
	seed := flag.Int64("seed", 1, "public deterministic fixture seed")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := run(ctx, [2]string{*mainRPC, *evmRPC}, *seed, json.NewEncoder(os.Stdout)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
