// semantic-coverage summarizes protocol-level behavior reached by either differential campaign.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

type transaction struct {
	Hash  common.Hash       `json:"hash"`
	Type  hexutil.Uint64    `json:"type"`
	To    *common.Address   `json:"to"`
	Input hexutil.Bytes     `json:"input"`
	Value *hexutil.Big      `json:"value"`
	Calls []transactionCall `json:"calls"`
}

type transactionCall struct {
	To    *common.Address `json:"to"`
	Input hexutil.Bytes   `json:"input"`
	Value *hexutil.Big    `json:"value"`
}

type block struct {
	Number       hexutil.Uint64 `json:"number"`
	Transactions []transaction  `json:"transactions"`
}

type receipt struct {
	Status          hexutil.Uint64    `json:"status"`
	ContractAddress *common.Address   `json:"contractAddress"`
	Logs            []json.RawMessage `json:"logs"`
}

type structLog struct {
	Depth int    `json:"depth"`
	Op    string `json:"op"`
}

type debugTrace struct {
	Failed     bool        `json:"failed"`
	StructLogs []structLog `json:"structLogs"`
}

type report struct {
	RPC                  string            `json:"rpc"`
	StartBlock           uint64            `json:"startBlock"`
	EndBlock             uint64            `json:"endBlock"`
	Blocks               uint64            `json:"blocks"`
	Transactions         uint64            `json:"transactions"`
	TransactionTypes     map[string]uint64 `json:"transactionTypes"`
	Successful           uint64            `json:"successful"`
	Reverted             uint64            `json:"reverted"`
	Creations            uint64            `json:"creations"`
	Calls                uint64            `json:"calls"`
	NonzeroValue         uint64            `json:"nonzeroValue"`
	InputSizeBuckets     map[string]uint64 `json:"inputSizeBuckets"`
	DestinationClasses   map[string]uint64 `json:"destinationClasses"`
	UniqueDestinations   uint64            `json:"uniqueDestinations"`
	Logs                 uint64            `json:"logs"`
	TempoAACalls         uint64            `json:"tempoAaCalls"`
	TempoAABatchSizes    map[string]uint64 `json:"tempoAaBatchSizes"`
	Selectors            map[string]uint64 `json:"selectors"`
	TracedTransactions   uint64            `json:"tracedTransactions"`
	EmptyOpcodeTraces    uint64            `json:"emptyOpcodeTraces"`
	MaxCallDepth         int               `json:"maxCallDepth"`
	UniqueOpcodes        []string          `json:"uniqueOpcodes"`
	SemanticOpcodeGroups map[string]uint64 `json:"semanticOpcodeGroups"`
}

func inputBucket(size int) string {
	switch {
	case size == 0:
		return "empty"
	case size <= 4:
		return "1-4"
	case size <= 32:
		return "5-32"
	case size <= 128:
		return "33-128"
	case size <= 512:
		return "129-512"
	default:
		return "513+"
	}
}

func destinationClass(to *common.Address) string {
	if to == nil {
		return "create"
	}
	hex := strings.TrimPrefix(strings.ToLower(to.Hex()), "0x")
	if strings.HasPrefix(hex, "20c") || strings.HasPrefix(hex, "dec") {
		return "tempo-system"
	}
	if to.Big().BitLen() <= 16 {
		return "ethereum-precompile-range"
	}
	return "ordinary"
}

func recordCall(r *report, destinations map[common.Address]struct{}, to *common.Address, input hexutil.Bytes, value *hexutil.Big) {
	r.InputSizeBuckets[inputBucket(len(input))]++
	r.DestinationClasses[destinationClass(to)]++
	if to == nil {
		r.Creations++
	} else {
		r.Calls++
		destinations[*to] = struct{}{}
	}
	if value != nil && value.ToInt().Sign() != 0 {
		r.NonzeroValue++
	}
	if len(input) >= 4 {
		r.Selectors[hexutil.Encode(input[:4])]++
	}
}

func opcodeGroup(op string) string {
	switch op {
	case "SLOAD", "SSTORE":
		return "persistent-storage"
	case "TLOAD", "TSTORE":
		return "transient-storage"
	case "CALL", "CALLCODE", "DELEGATECALL", "STATICCALL", "EXTCALL", "EXTDELEGATECALL", "EXTSTATICCALL":
		return "external-call"
	case "CREATE", "CREATE2", "EOFCREATE", "RETURNCONTRACT":
		return "contract-creation"
	case "SELFDESTRUCT":
		return "selfdestruct"
	case "LOG0", "LOG1", "LOG2", "LOG3", "LOG4":
		return "logs"
	case "BLOBHASH", "BLOBBASEFEE":
		return "blob-context"
	case "BLOCKHASH", "COINBASE", "TIMESTAMP", "NUMBER", "PREVRANDAO", "GASLIMIT", "CHAINID", "BASEFEE":
		return "block-context"
	case "REVERT", "INVALID":
		return "exceptional-exit"
	default:
		return ""
	}
}

func main() {
	endpoint := flag.String("rpc", "http://127.0.0.1:8545", "execution RPC endpoint")
	window := flag.Uint64("blocks", 256, "number of recent blocks to summarize")
	traceSamples := flag.Int("trace-samples", 32, "transactions to sample with debug_traceTransaction")
	timeout := flag.Duration("timeout", 5*time.Minute, "whole probe timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client, err := rpc.DialContext(ctx, *endpoint)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()

	var head hexutil.Uint64
	if err := client.CallContext(ctx, &head, "eth_blockNumber"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	end := uint64(head)
	start := uint64(1)
	if end >= *window {
		start = end - *window + 1
	}
	r := report{
		RPC: *endpoint, StartBlock: start, EndBlock: end,
		TransactionTypes: map[string]uint64{}, InputSizeBuckets: map[string]uint64{},
		DestinationClasses: map[string]uint64{}, TempoAABatchSizes: map[string]uint64{},
		Selectors: map[string]uint64{}, SemanticOpcodeGroups: map[string]uint64{},
	}
	destinations := map[common.Address]struct{}{}
	hashesByType := map[string][]common.Hash{}
	for number := start; number <= end; number++ {
		var b *block
		if err := client.CallContext(ctx, &b, "eth_getBlockByNumber", hexutil.EncodeUint64(number), true); err != nil || b == nil {
			fmt.Fprintf(os.Stderr, "block %d: %v\n", number, err)
			os.Exit(1)
		}
		r.Blocks++
		r.Transactions += uint64(len(b.Transactions))
		var receipts []receipt
		if err := client.CallContext(ctx, &receipts, "eth_getBlockReceipts", hexutil.EncodeUint64(number)); err != nil {
			fmt.Fprintf(os.Stderr, "receipts %d: %v\n", number, err)
			os.Exit(1)
		}
		for i, tx := range b.Transactions {
			txType := hexutil.EncodeUint64(uint64(tx.Type))
			r.TransactionTypes[txType]++
			if uint64(tx.Type) == 0x76 && len(tx.Calls) > 0 {
				r.TempoAABatchSizes[fmt.Sprint(len(tx.Calls))]++
				for _, call := range tx.Calls {
					r.TempoAACalls++
					recordCall(&r, destinations, call.To, call.Input, call.Value)
				}
			} else {
				recordCall(&r, destinations, tx.To, tx.Input, tx.Value)
			}
			if i < len(receipts) {
				if uint64(receipts[i].Status) == 1 {
					r.Successful++
				} else {
					r.Reverted++
				}
				r.Logs += uint64(len(receipts[i].Logs))
			}
			hashesByType[txType] = append(hashesByType[txType], tx.Hash)
		}
	}
	r.UniqueDestinations = uint64(len(destinations))
	opcodes := map[string]struct{}{}
	var sampled []common.Hash
	perType := max(1, *traceSamples/max(1, len(hashesByType)))
	for _, hashes := range hashesByType {
		count := min(perType, len(hashes))
		for i := 0; i < count; i++ {
			sampled = append(sampled, hashes[i*len(hashes)/count])
		}
	}
	for _, hash := range sampled {
		var trace debugTrace
		config := map[string]bool{"disableMemory": true, "disableStack": true, "disableStorage": true, "enableReturnData": false}
		if err := client.CallContext(ctx, &trace, "debug_traceTransaction", hash, config); err != nil {
			fmt.Fprintf(os.Stderr, "trace %s: %v\n", hash, err)
			os.Exit(1)
		}
		r.TracedTransactions++
		if len(trace.StructLogs) == 0 {
			r.EmptyOpcodeTraces++
		}
		for _, step := range trace.StructLogs {
			opcodes[step.Op] = struct{}{}
			if step.Depth > r.MaxCallDepth {
				r.MaxCallDepth = step.Depth
			}
			if group := opcodeGroup(step.Op); group != "" {
				r.SemanticOpcodeGroups[group]++
			}
		}
	}
	for op := range opcodes {
		r.UniqueOpcodes = append(r.UniqueOpcodes, op)
	}
	sort.Strings(r.UniqueOpcodes)
	if err := json.NewEncoder(os.Stdout).Encode(r); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
