package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tempoxyz/tempo-go/pkg/precompiles"
	"github.com/tempoxyz/tempo-go/pkg/transaction"
)

func TestLocalEndpoint(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:8545", "http://[::1]:8545"} {
		if err := localEndpoint(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"https://127.0.0.1:8545", "http://localhost:8545", "http://rpc.example:8545", "http://192.168.1.1:8545", "http://127.0.0.1.example:8545", "http://user@127.0.0.1:8545", ""} {
		if err := localEndpoint(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

// This mock checks orchestration and the independent balance assertion, not
// protocol correctness. Actual node execution is a separate CI job.
func mockChain(t *testing.T, gas uint64, corruptBalance bool) *httptest.Server {
	t.Helper()
	var balance big.Int
	contract, err := precompiles.ABI("ITIP20")
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch req.Method {
		case "eth_chainId":
			result = "0x539"
		case "eth_getTransactionCount":
			result = "0x0"
		case "tempo_fundAddress":
			result = []common.Hash{{1}}
		case "eth_getTransactionReceipt":
			result = receipt{Status: 1, GasUsed: hexutil.Uint64(gas), Logs: []logEntry{}}
		case "eth_sendRawTransaction":
			var raw string
			if err := json.Unmarshal(req.Params[0], &raw); err != nil {
				t.Error(err)
				return
			}
			tx, err := transaction.Deserialize(raw)
			if err != nil {
				t.Error(err)
				return
			}
			sender, err := transaction.VerifySignature(tx)
			if err != nil {
				t.Error(err)
				return
			}
			if tx.FeePayerSignature != nil {
				if _, err := transaction.VerifyFeePayerSignature(tx, sender); err != nil {
					t.Error(err)
					return
				}
			}
			for _, call := range tx.Calls {
				method, err := contract.MethodById(call.Data[:4])
				if err != nil {
					t.Error(err)
					return
				}
				args, err := method.Inputs.Unpack(call.Data[4:])
				if err != nil {
					t.Error(err)
					return
				}
				if method.Name == "transfer" || method.Name == "transferWithMemo" {
					balance.Add(&balance, args[1].(*big.Int))
				}
			}
			result = crypto.Keccak256Hash(common.FromHex(raw))
		case "eth_call":
			n := new(big.Int).Set(&balance)
			if corruptBalance {
				n.Add(n, big.NewInt(1))
			}
			result = fmt.Sprintf("0x%064x", n)
		default:
			t.Errorf("unexpected RPC %s", req.Method)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
}

func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		name    string
		gas     uint64
		corrupt bool
		want    string
	}{
		{"equal", 21000, false, ""},
		{"gas differs", 21001, false, "receipt/state mismatch"},
		{"both balances wrong", 21000, true, "recipient balance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := mockChain(t, 21000, tc.corrupt)
			defer a.Close()
			b := mockChain(t, tc.gas, tc.corrupt)
			defer b.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var output bytes.Buffer
			err := run(ctx, [2]string{a.URL, b.URL}, 1, json.NewEncoder(&output))
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("got %v, want %s", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), `"passed":23`) {
				t.Fatal(output.String())
			}
		})
	}
}
