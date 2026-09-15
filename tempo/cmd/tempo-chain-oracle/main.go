// tempo-chain-oracle verifies that two EVM engines import the same chain and
// return identical Parity-style opcode traces. It is used by both the Ethereum
// and Tempo compatibility workflows.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

type block struct {
	Number       hexutil.Uint64 `json:"number"`
	Hash         common.Hash    `json:"hash"`
	ParentHash   common.Hash    `json:"parentHash"`
	StateRoot    common.Hash    `json:"stateRoot"`
	ReceiptsRoot common.Hash    `json:"receiptsRoot"`
	Transactions []common.Hash  `json:"transactions"`
}

type nodeInfo struct {
	Enode string `json:"enode"`
}

type evidence struct {
	Block        uint64      `json:"block"`
	Hash         common.Hash `json:"hash"`
	StateRoot    common.Hash `json:"stateRoot"`
	ReceiptsRoot common.Hash `json:"receiptsRoot"`
	Transactions int         `json:"transactions"`
}

func dial(ctx context.Context, endpoint string, expectedChain uint64) (*rpc.Client, uint64, error) {
	c, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		return nil, 0, err
	}
	var chain hexutil.Uint64
	if err := c.CallContext(ctx, &chain, "eth_chainId"); err != nil {
		c.Close()
		return nil, 0, err
	}
	if expectedChain != 0 && uint64(chain) != expectedChain {
		c.Close()
		return nil, 0, fmt.Errorf("%s: expected chain %d, got %d", endpoint, expectedChain, chain)
	}
	return c, uint64(chain), nil
}

func connectPeer(ctx context.Context, producer, verifier *rpc.Client, producerEndpoint string) error {
	var info nodeInfo
	if err := producer.CallContext(ctx, &info, "admin_nodeInfo"); err != nil {
		return fmt.Errorf("producer admin_nodeInfo: %w", err)
	}
	if info.Enode == "" {
		return errors.New("producer admin_nodeInfo returned an empty enode")
	}
	// Node records produced inside containers may advertise loopback. Preserve
	// the node ID but replace the address with the producer's enclave DNS name.
	enode, err := url.Parse(info.Enode)
	if err != nil {
		return fmt.Errorf("parse producer enode: %w", err)
	}
	rpcURL, err := url.Parse(producerEndpoint)
	if err != nil {
		return fmt.Errorf("parse producer RPC URL: %w", err)
	}
	hosts, err := net.DefaultResolver.LookupHost(ctx, rpcURL.Hostname())
	if err != nil {
		return fmt.Errorf("resolve producer host %s: %w", rpcURL.Hostname(), err)
	}
	if len(hosts) == 0 {
		return fmt.Errorf("resolve producer host %s: no addresses", rpcURL.Hostname())
	}
	host := hosts[0]
	for _, candidate := range hosts {
		if net.ParseIP(candidate).To4() != nil {
			host = candidate
			break
		}
	}
	enode.Host = net.JoinHostPort(host, "30303")
	var added bool
	if err := verifier.CallContext(ctx, &added, "admin_addPeer", enode.String()); err != nil {
		return fmt.Errorf("verifier admin_addPeer: %w", err)
	}
	if !added {
		return errors.New("verifier refused producer devp2p peer")
	}
	return nil
}

func height(ctx context.Context, c *rpc.Client, headTag string) (uint64, error) {
	if headTag == "latest" {
		var number hexutil.Uint64
		if err := c.CallContext(ctx, &number, "eth_blockNumber"); err != nil {
			return 0, err
		}
		return uint64(number), nil
	}
	var head *block
	if err := c.CallContext(ctx, &head, "eth_getBlockByNumber", headTag, false); err != nil {
		return 0, err
	}
	if head == nil {
		return 0, fmt.Errorf("%s head is unavailable", headTag)
	}
	return uint64(head.Number), nil
}

func getBlock(ctx context.Context, c *rpc.Client, number uint64) (*block, error) {
	var got *block
	if err := c.CallContext(ctx, &got, "eth_getBlockByNumber", hexutil.EncodeUint64(number), false); err != nil {
		return nil, err
	}
	if got == nil {
		return nil, fmt.Errorf("block %d is unavailable", number)
	}
	return got, nil
}

func canonicalRPC(ctx context.Context, c *rpc.Client, method string, params ...any) ([]byte, error) {
	var raw json.RawMessage
	if err := c.CallContext(ctx, &raw, method, params...); err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%s returned invalid JSON: %w", method, err)
	}
	return json.Marshal(value)
}

func compareRPC(ctx context.Context, clients [2]*rpc.Client, method string, params ...any) error {
	left, err := canonicalRPC(ctx, clients[0], method, params...)
	if err != nil {
		return fmt.Errorf("revm %s: %w", method, err)
	}
	right, err := canonicalRPC(ctx, clients[1], method, params...)
	if err != nil {
		return fmt.Errorf("evm2 %s: %w", method, err)
	}
	if !reflect.DeepEqual(left, right) {
		return fmt.Errorf("%s divergence for params %v\nrevm=%s\nevm2=%s", method, params, left, right)
	}
	return nil
}

func compareBlock(ctx context.Context, clients [2]*rpc.Client, number uint64, compareTraces bool, out *json.Encoder) error {
	left, err := getBlock(ctx, clients[0], number)
	if err != nil {
		return fmt.Errorf("revm block %d: %w", number, err)
	}
	right, err := getBlock(ctx, clients[1], number)
	if err != nil {
		return fmt.Errorf("evm2 block %d: %w", number, err)
	}
	if !reflect.DeepEqual(left, right) {
		return fmt.Errorf("canonical block %d divergence\nrevm=%+v\nevm2=%+v", number, left, right)
	}
	if compareTraces {
		traceTypes := []string{"vmTrace"}
		if err := compareRPC(ctx, clients, "trace_replayBlockTransactions", hexutil.EncodeUint64(number), traceTypes); err != nil {
			return err
		}
		// The block method already covers every transaction. Sample one transaction
		// to exercise the separate lookup/response path without doubling trace cost.
		if len(left.Transactions) > 0 {
			if err := compareRPC(ctx, clients, "trace_replayTransaction", left.Transactions[0], traceTypes); err != nil {
				return err
			}
		}
	}
	return out.Encode(evidence{number, left.Hash, left.StateRoot, left.ReceiptsRoot, len(left.Transactions)})
}

func run(ctx context.Context, endpoints [2]string, expectedChain uint64, connectPeers, compareTraces bool, headTag string, minBlocks, maxLag uint64, exitAfterMinimum bool, poll, stall time.Duration, out *json.Encoder) error {
	var clients [2]*rpc.Client
	var chainIDs [2]uint64
	for i, endpoint := range endpoints {
		c, chainID, err := dial(ctx, endpoint, expectedChain)
		if err != nil {
			return err
		}
		defer c.Close()
		clients[i] = c
		chainIDs[i] = chainID
	}
	if chainIDs[0] != chainIDs[1] {
		return fmt.Errorf("chain ID divergence: left=%d right=%d", chainIDs[0], chainIDs[1])
	}
	if connectPeers {
		if err := connectPeer(ctx, clients[0], clients[1], endpoints[0]); err != nil {
			return err
		}
	}
	if err := out.Encode(map[string]any{"event": "started", "left": endpoints[0], "right": endpoints[1], "chainId": chainIDs[0], "headTag": headTag, "connectedPeers": connectPeers, "compareTraces": compareTraces}); err != nil {
		return err
	}

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	lastProgress := time.Now()
	var compared uint64
	var transactions uint64
	for {
		producerHeight, err := height(ctx, clients[0], headTag)
		if err != nil {
			return fmt.Errorf("producer height: %w", err)
		}
		verifierHeight, err := height(ctx, clients[1], headTag)
		if err != nil {
			return fmt.Errorf("verifier height: %w", err)
		}
		commonHeight := min(producerHeight, verifierHeight)
		for compared < commonHeight {
			compared++
			left, err := getBlock(ctx, clients[0], compared)
			if err != nil {
				return fmt.Errorf("revm block %d: %w", compared, err)
			}
			if err := compareBlock(ctx, clients, compared, compareTraces, out); err != nil {
				return err
			}
			transactions += uint64(len(left.Transactions))
			lastProgress = time.Now()
		}
		if exitAfterMinimum && compared >= minBlocks {
			if transactions == 0 {
				return errors.New("no transactions were included")
			}
			return out.Encode(map[string]any{"event": "passed", "comparedBlocks": compared, "includedTransactions": transactions})
		}
		if time.Since(lastProgress) > stall {
			return fmt.Errorf("chain stalled: compared=%d producer=%d verifier=%d for %s", compared, producerHeight, verifierHeight, stall)
		}
		select {
		case <-ctx.Done():
			if compared < minBlocks {
				return fmt.Errorf("campaign ended after %d blocks, need at least %d", compared, minBlocks)
			}
			if transactions == 0 {
				return errors.New("no transactions were included")
			}
			if producerHeight > compared+maxLag || verifierHeight > compared+maxLag {
				return fmt.Errorf("oracle did not cover the generated tail: compared=%d left=%d right=%d max-lag=%d", compared, producerHeight, verifierHeight, maxLag)
			}
			return out.Encode(map[string]any{"event": "passed", "comparedBlocks": compared, "includedTransactions": transactions})
		case <-ticker.C:
		}
	}
}

func main() {
	producer := flag.String("left-rpc", "http://tempo-revm:8545", "revm/control RPC")
	verifier := flag.String("right-rpc", "http://tempo-evm2:8545", "EVM2/candidate RPC")
	chainID := flag.Uint64("chain-id", 0, "required chain ID; zero accepts any matching pair")
	connectPeers := flag.Bool("connect-peers", false, "connect right to left with admin_addPeer")
	compareTraces := flag.Bool("compare-rpc-traces", false, "also compare Parity vmTrace RPC responses")
	headTag := flag.String("head-tag", "latest", "comparison head: latest, safe, or finalized")
	duration := flag.Duration("duration", 5*time.Minute, "continuous comparison duration")
	minBlocks := flag.Uint64("min-blocks", 32, "minimum canonical blocks compared")
	maxLag := flag.Uint64("max-lag", 5, "maximum unexamined blocks allowed at timed completion")
	exitAfterMinimum := flag.Bool("exit-after-min-blocks", false, "exit successfully once the minimum is compared")
	poll := flag.Duration("poll", 250*time.Millisecond, "head poll interval")
	stall := flag.Duration("stall-timeout", 30*time.Second, "maximum time without a newly compared block")
	flag.Parse()
	if *headTag != "latest" && *headTag != "safe" && *headTag != "finalized" {
		fmt.Fprintln(os.Stderr, "head-tag must be latest, safe, or finalized")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()
	if err := run(ctx, [2]string{*producer, *verifier}, *chainID, *connectPeers, *compareTraces, *headTag, *minBlocks, *maxLag, *exitAfterMinimum, *poll, *stall, json.NewEncoder(os.Stdout)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
