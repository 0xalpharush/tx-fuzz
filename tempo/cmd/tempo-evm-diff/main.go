// tempo-evm-diff executes deterministic FuzzyVM programs on two disposable Tempo localnets.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	tempofuzz "github.com/0xalpharush/tx-fuzz/tempo"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/tempoxyz/tempo-go/pkg/precompiles"
	"github.com/tempoxyz/tempo-go/pkg/signer"
	"github.com/tempoxyz/tempo-go/pkg/transaction"
)

type outcomeClass string

const (
	outcomeAccepted outcomeClass = "accepted"
	outcomeReverted outcomeClass = "reverted"
	outcomeRejected outcomeClass = "rejected"
)

type logEntry struct {
	Address common.Address `json:"address"`
	Topics  []common.Hash  `json:"topics"`
	Data    hexutil.Bytes  `json:"data"`
}

type wireReceipt struct {
	BlockHash common.Hash    `json:"blockHash"`
	Status    hexutil.Uint64 `json:"status"`
	GasUsed   hexutil.Uint64 `json:"gasUsed"`
	Logs      []logEntry     `json:"logs"`
}

type block struct {
	StateRoot common.Hash `json:"stateRoot"`
}

type receiptResult struct {
	Status  uint64     `json:"status"`
	GasUsed uint64     `json:"gasUsed"`
	Logs    []logEntry `json:"logs"`
}

type snapshot struct {
	StateRoot         common.Hash `json:"stateRoot"`
	SenderNonce       uint64      `json:"senderNonce"`
	ParallelNonce     string      `json:"parallelNonce"`
	SenderPathUSD     string      `json:"senderPathUSD"`
	SenderAlphaUSD    string      `json:"senderAlphaUSD"`
	RecipientAlphaUSD string      `json:"recipientAlphaUSD"`
	ContractCode      string      `json:"contractCode"`
	Storage           [4]string   `json:"storage"`
}

type execution struct {
	Class   outcomeClass   `json:"class"`
	Error   string         `json:"error,omitempty"`
	Receipt *receiptResult `json:"receipt,omitempty"`
	State   *snapshot      `json:"state,omitempty"`
}

type mismatch struct {
	Seed      int64     `json:"seed"`
	Index     int       `json:"index"`
	ProgramID string    `json:"programId"`
	Phase     string    `json:"phase"`
	Runtime   string    `json:"runtime"`
	Calldata  string    `json:"calldata"`
	Raw       string    `json:"rawTransaction"`
	Baseline  execution `json:"baseline"`
	Candidate execution `json:"candidate"`
}

func parseEndpoint(raw, enclaveService string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(u.Hostname())
	isLoopback := ip != nil && ip.IsLoopback()
	isEnclaveService := enclaveService != "" && u.Hostname() == enclaveService
	if u.Scheme != "http" || (!isLoopback && !isEnclaveService) || u.User != nil {
		return nil, fmt.Errorf("endpoint must be loopback or the %q enclave service: %q", enclaveService, raw)
	}
	return u, nil
}

func dialEndpoint(ctx context.Context, endpoint, enclaveService string) (*rpc.Client, error) {
	if _, err := parseEndpoint(endpoint, enclaveService); err != nil {
		return nil, err
	}
	c, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var chain hexutil.Uint64
	if err := c.CallContext(ctx, &chain, "eth_chainId"); err != nil {
		c.Close()
		return nil, err
	}
	if chain != 1337 {
		c.Close()
		return nil, fmt.Errorf("expected disposable localnet chain 1337, got %d", chain)
	}
	return c, nil
}

func waitReceipt(ctx context.Context, c *rpc.Client, hash common.Hash) (*wireReceipt, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var receipt *wireReceipt
		if err := c.CallContext(ctx, &receipt, "eth_getTransactionReceipt", hash); err != nil {
			return nil, err
		}
		if receipt != nil {
			if receipt.Logs == nil {
				receipt.Logs = []logEntry{}
			}
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func fund(ctx context.Context, c *rpc.Client, address common.Address) error {
	var hashes []common.Hash
	if err := c.CallContext(ctx, &hashes, "tempo_fundAddress", address); err != nil {
		return err
	}
	if len(hashes) == 0 {
		return errors.New("faucet returned no transactions")
	}
	for _, hash := range hashes {
		r, err := waitReceipt(ctx, c, hash)
		if err != nil {
			return err
		}
		if r.Status != 1 {
			return errors.New("faucet transaction reverted")
		}
	}
	return nil
}

func tokenBalance(ctx context.Context, c *rpc.Client, token, owner common.Address) (string, error) {
	call, err := precompiles.Call("ITIP20", token, "balanceOf", owner)
	if err != nil {
		return "", err
	}
	var value hexutil.Bytes
	if err := c.CallContext(ctx, &value, "eth_call", map[string]any{
		"to": token, "data": hexutil.Encode(call.Data),
	}, "latest"); err != nil {
		return "", err
	}
	return new(big.Int).SetBytes(value).String(), nil
}

func parallelNonce(ctx context.Context, c *rpc.Client, owner common.Address) (string, error) {
	address, ok := precompiles.Address("INonce")
	if !ok {
		return "", errors.New("INonce does not have a fixed address")
	}
	call, err := precompiles.Call("INonce", address, "getNonce", owner, big.NewInt(42))
	if err != nil {
		return "", err
	}
	var value hexutil.Bytes
	if err := c.CallContext(ctx, &value, "eth_call", map[string]any{
		"to": address, "data": hexutil.Encode(call.Data),
	}, "latest"); err != nil {
		return "", err
	}
	return new(big.Int).SetBytes(value).String(), nil
}

func capture(ctx context.Context, c *rpc.Client, r *wireReceipt, sender, recipient, contract common.Address) (*snapshot, error) {
	var b block
	if err := c.CallContext(ctx, &b, "eth_getBlockByHash", r.BlockHash, false); err != nil {
		return nil, err
	}
	var nonce hexutil.Uint64
	if err := c.CallContext(ctx, &nonce, "eth_getTransactionCount", sender, "latest"); err != nil {
		return nil, err
	}
	path, err := tokenBalance(ctx, c, transaction.PathUSDAddress, sender)
	if err != nil {
		return nil, err
	}
	alpha, err := tokenBalance(ctx, c, transaction.AlphaUSDAddress, sender)
	if err != nil {
		return nil, err
	}
	recipientAlpha, err := tokenBalance(ctx, c, transaction.AlphaUSDAddress, recipient)
	if err != nil {
		return nil, err
	}
	pnonce, err := parallelNonce(ctx, c, sender)
	if err != nil {
		return nil, err
	}
	var code hexutil.Bytes
	if err := c.CallContext(ctx, &code, "eth_getCode", contract, "latest"); err != nil {
		return nil, err
	}
	s := &snapshot{
		StateRoot: b.StateRoot, SenderNonce: uint64(nonce), ParallelNonce: pnonce,
		SenderPathUSD: path, SenderAlphaUSD: alpha, RecipientAlphaUSD: recipientAlpha,
		ContractCode: hexutil.Encode(code),
	}
	for i := range s.Storage {
		var slot hexutil.Bytes
		if err := c.CallContext(ctx, &slot, "eth_getStorageAt", contract, hexutil.EncodeUint64(uint64(i)), "latest"); err != nil {
			return nil, err
		}
		s.Storage[i] = hexutil.Encode(slot)
	}
	return s, nil
}

func execute(ctx context.Context, c *rpc.Client, raw string, sender, recipient, contract common.Address) (execution, error) {
	var hash common.Hash
	if err := c.CallContext(ctx, &hash, "eth_sendRawTransaction", raw); err != nil {
		return execution{Class: outcomeRejected, Error: normalizeRPCError(err)}, nil
	}
	r, err := waitReceipt(ctx, c, hash)
	if err != nil {
		return execution{}, err
	}
	class := outcomeAccepted
	if r.Status == 0 {
		class = outcomeReverted
	}
	state, err := capture(ctx, c, r, sender, recipient, contract)
	if err != nil {
		return execution{}, err
	}
	return execution{
		Class:   class,
		Receipt: &receiptResult{Status: uint64(r.Status), GasUsed: uint64(r.GasUsed), Logs: r.Logs},
		State:   state,
	}, nil
}

func normalizeRPCError(err error) string {
	var coded rpc.Error
	if errors.As(err, &coded) {
		return fmt.Sprintf("code=%d", coded.ErrorCode())
	}
	return strings.ToLower(err.Error())
}

func equalExecution(a, b execution) bool {
	if a.Class != b.Class {
		return false
	}
	if a.Class == outcomeRejected {
		return true
	}
	return reflect.DeepEqual(a.Receipt, b.Receipt) && reflect.DeepEqual(a.State, b.State)
}

func nextNonce(ctx context.Context, clients [2]*rpc.Client, sender common.Address) (uint64, error) {
	var nonces [2]hexutil.Uint64
	for i, c := range clients {
		if err := c.CallContext(ctx, &nonces[i], "eth_getTransactionCount", sender, "latest"); err != nil {
			return 0, err
		}
	}
	if nonces[0] != nonces[1] {
		return 0, fmt.Errorf("sender nonce divergence: baseline=%d candidate=%d", nonces[0], nonces[1])
	}
	return uint64(nonces[0]), nil
}

func signedRaw(s *signer.Signer, nonce uint64, call transaction.Call) (string, error) {
	tx := transaction.NewDefault(1337)
	tx.Nonce = nonce
	tx.Gas = 5_000_000
	tx.MaxFeePerGas.SetUint64(20_000_000_000)
	tx.MaxPriorityFeePerGas.SetUint64(20_000_000_000)
	tx.Calls = []transaction.Call{call}
	if err := transaction.SignTransaction(tx, s); err != nil {
		return "", err
	}
	return transaction.Serialize(tx, nil)
}

func comparePhase(ctx context.Context, clients [2]*rpc.Client, raw string, sender, recipient, contract common.Address) ([2]execution, error) {
	var got [2]execution
	for i, c := range clients {
		result, err := execute(ctx, c, raw, sender, recipient, contract)
		if err != nil {
			return got, fmt.Errorf("chain %d: %w", i, err)
		}
		got[i] = result
	}
	return got, nil
}

func run(ctx context.Context, endpoints [2]string, seed int64, count, maxCodeBytes int, out *json.Encoder) error {
	var clients [2]*rpc.Client
	services := [2]string{"baseline", "candidate"}
	for i, endpoint := range endpoints {
		c, err := dialEndpoint(ctx, endpoint, services[i])
		if err != nil {
			return err
		}
		defer c.Close()
		clients[i] = c
	}
	var versions [2]string
	for i, c := range clients {
		if err := c.CallContext(ctx, &versions[i], "web3_clientVersion"); err != nil {
			return err
		}
	}
	key, err := crypto.ToECDSA(crypto.Keccak256([]byte(fmt.Sprintf("tempo evm differential %d", seed))))
	if err != nil {
		return err
	}
	s := signer.NewSignerFromKey(key)
	recipient := common.HexToAddress("0x1111111111111111111111111111111111111111")
	for _, c := range clients {
		if err := fund(ctx, c, s.Address()); err != nil {
			return fmt.Errorf("fund fixture account: %w", err)
		}
	}
	programs, err := tempofuzz.GeneratePrograms(seed, count, maxCodeBytes)
	if err != nil {
		return err
	}
	if err := out.Encode(map[string]any{"seed": seed, "programs": count, "baselineClient": versions[0], "candidateClient": versions[1]}); err != nil {
		return err
	}
	phases := 0
	for _, program := range programs {
		if program.Index > 0 && program.Index%16 == 0 {
			for _, c := range clients {
				if err := fund(ctx, c, s.Address()); err != nil {
					return fmt.Errorf("refill fixture account: %w", err)
				}
			}
		}
		nonce, err := nextNonce(ctx, clients, s.Address())
		if err != nil {
			return err
		}
		contract := crypto.CreateAddress(s.Address(), nonce)
		initcode, err := tempofuzz.RuntimeInitCode(program.Runtime)
		if err != nil {
			return err
		}
		deployRaw, err := signedRaw(s, nonce, transaction.Call{Value: new(big.Int), Data: initcode})
		if err != nil {
			return err
		}
		deploy, err := comparePhase(ctx, clients, deployRaw, s.Address(), recipient, contract)
		if err != nil {
			return err
		}
		phases++
		if !equalExecution(deploy[0], deploy[1]) {
			_ = out.Encode(mismatch{seed, program.Index, program.ID, "deploy", hex.EncodeToString(program.Runtime), hex.EncodeToString(program.Calldata), deployRaw, deploy[0], deploy[1]})
			return fmt.Errorf("program %d (%s) deploy divergence", program.Index, program.ID)
		}
		if deploy[0].Class != outcomeAccepted {
			if err := out.Encode(map[string]any{"index": program.Index, "programId": program.ID, "deploy": deploy[0].Class}); err != nil {
				return err
			}
			continue
		}
		nonce, err = nextNonce(ctx, clients, s.Address())
		if err != nil {
			return err
		}
		callRaw, err := signedRaw(s, nonce, transaction.Call{To: &contract, Value: new(big.Int), Data: program.Calldata})
		if err != nil {
			return err
		}
		call, err := comparePhase(ctx, clients, callRaw, s.Address(), recipient, contract)
		if err != nil {
			return err
		}
		phases++
		if !equalExecution(call[0], call[1]) {
			_ = out.Encode(mismatch{seed, program.Index, program.ID, "call", hex.EncodeToString(program.Runtime), hex.EncodeToString(program.Calldata), callRaw, call[0], call[1]})
			return fmt.Errorf("program %d (%s) call divergence", program.Index, program.ID)
		}
		if err := out.Encode(map[string]any{"index": program.Index, "programId": program.ID, "deploy": deploy[0].Class, "call": call[0].Class, "gasUsed": call[0].Receipt.GasUsed}); err != nil {
			return err
		}
	}
	return out.Encode(map[string]any{"passedPrograms": len(programs), "comparedPhases": phases, "seed": seed})
}

// runSingle continuously submits randomized FuzzyVM programs to one canonical
// producer. A validating peer imports those blocks and is checked separately by
// evm-rpc-oracle. Raw transactions are retained in JSONL for exact replay.
func runSingle(ctx context.Context, endpoint string, seed int64, count, maxCodeBytes int, out *json.Encoder) error {
	c, err := dialEndpoint(ctx, endpoint, "tempo-revm")
	if err != nil {
		return err
	}
	defer c.Close()
	key, err := crypto.ToECDSA(crypto.Keccak256([]byte(fmt.Sprintf("tempo evm differential %d", seed))))
	if err != nil {
		return err
	}
	s := signer.NewSignerFromKey(key)
	recipient := common.HexToAddress("0x1111111111111111111111111111111111111111")
	if err := fund(ctx, c, s.Address()); err != nil {
		return fmt.Errorf("fund fixture account: %w", err)
	}

	completed := 0
	for round := int64(0); ; round++ {
		select {
		case <-ctx.Done():
			if completed == 0 {
				return ctx.Err()
			}
			return out.Encode(map[string]any{"event": "passed", "submittedPrograms": completed, "lastSeed": seed + round - 1})
		default:
		}
		programs, err := tempofuzz.GeneratePrograms(seed+round, count, maxCodeBytes)
		if err != nil {
			return err
		}
		for _, program := range programs {
			select {
			case <-ctx.Done():
				return out.Encode(map[string]any{"event": "passed", "submittedPrograms": completed, "lastSeed": seed + round})
			default:
			}
			if completed > 0 && completed%16 == 0 {
				if err := fund(ctx, c, s.Address()); err != nil {
					return fmt.Errorf("refill fixture account: %w", err)
				}
			}
			var nonce hexutil.Uint64
			if err := c.CallContext(ctx, &nonce, "eth_getTransactionCount", s.Address(), "latest"); err != nil {
				return err
			}
			contract := crypto.CreateAddress(s.Address(), uint64(nonce))
			initcode, err := tempofuzz.RuntimeInitCode(program.Runtime)
			if err != nil {
				return err
			}
			deployRaw, err := signedRaw(s, uint64(nonce), transaction.Call{Value: new(big.Int), Data: initcode})
			if err != nil {
				return err
			}
			deploy, err := execute(ctx, c, deployRaw, s.Address(), recipient, contract)
			if err != nil {
				return err
			}
			record := map[string]any{
				"seed": seed + round, "index": program.Index, "programId": program.ID,
				"phase": "deploy", "runtime": hex.EncodeToString(program.Runtime),
				"calldata": hex.EncodeToString(program.Calldata), "rawTransaction": deployRaw,
				"outcome": deploy.Class,
			}
			if err := out.Encode(record); err != nil {
				return err
			}
			if deploy.Class == outcomeAccepted {
				if err := c.CallContext(ctx, &nonce, "eth_getTransactionCount", s.Address(), "latest"); err != nil {
					return err
				}
				callRaw, err := signedRaw(s, uint64(nonce), transaction.Call{To: &contract, Value: new(big.Int), Data: program.Calldata})
				if err != nil {
					return err
				}
				call, err := execute(ctx, c, callRaw, s.Address(), recipient, contract)
				if err != nil {
					return err
				}
				if err := out.Encode(map[string]any{
					"seed": seed + round, "index": program.Index, "programId": program.ID,
					"phase": "call", "runtime": hex.EncodeToString(program.Runtime),
					"calldata": hex.EncodeToString(program.Calldata), "rawTransaction": callRaw,
					"outcome": call.Class,
				}); err != nil {
					return err
				}
			}
			completed++
		}
	}
}

func main() {
	baselineRPC := flag.String("baseline-rpc", "", "fresh main/revm localnet RPC (loopback only)")
	candidateRPC := flag.String("candidate-rpc", "", "fresh EVM2 localnet RPC (loopback only)")
	singleRPC := flag.String("single-rpc", "", "canonical producer RPC for continuous same-chain fuzzing")
	seed := flag.Int64("seed", 1, "public deterministic fixture seed")
	programs := flag.Int("programs", 128, "number of generated EVM programs")
	maxCodeBytes := flag.Int("max-code-bytes", 512, "maximum generated runtime size")
	timeout := flag.Duration("timeout", 15*time.Minute, "whole-campaign timeout")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if *singleRPC != "" {
		if err := runSingle(ctx, *singleRPC, *seed, *programs, *maxCodeBytes, json.NewEncoder(os.Stdout)); err != nil &&
			!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(ctx, [2]string{*baselineRPC, *candidateRPC}, *seed, *programs, *maxCodeBytes, json.NewEncoder(os.Stdout)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
