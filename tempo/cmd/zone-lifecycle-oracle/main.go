package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

var (
	depositMade         = crypto.Keccak256Hash([]byte("DepositMade(bytes32,address,address,uint128,uint128,uint256,bytes32,uint8,bytes,bytes12,bytes16,address,uint64)"))
	depositProcessed    = crypto.Keccak256Hash([]byte("DepositProcessed(bytes32,address,address,address,uint128,bytes32)"))
	depositFailed       = crypto.Keccak256Hash([]byte("DepositFailed(bytes32,address,address,uint128)"))
	depositRejected     = crypto.Keccak256Hash([]byte("DepositRejected(bytes32,address,uint8,address,uint128,address)"))
	withdrawalRequested = crypto.Keccak256Hash([]byte("WithdrawalRequested(uint64,address,address,address,uint128,uint128,bytes32,uint64,uint64,bytes,bytes)"))
	withdrawalProcessed = crypto.Keccak256Hash([]byte("WithdrawalProcessed(address,bytes32,address,uint128,bool)"))
)

type logEntry struct{}

type blockHeader struct {
	Hash      common.Hash `json:"hash"`
	StateRoot common.Hash `json:"stateRoot"`
}

func height(ctx context.Context, client *rpc.Client) (uint64, error) {
	var result hexutil.Uint64
	if err := client.CallContext(ctx, &result, "eth_blockNumber"); err != nil {
		return 0, err
	}
	return uint64(result), nil
}

func block(ctx context.Context, client *rpc.Client, number uint64) (*blockHeader, error) {
	var result *blockHeader
	if err := client.CallContext(ctx, &result, "eth_getBlockByNumber", hexutil.EncodeUint64(number), false); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("block %d is unavailable", number)
	}
	return result, nil
}

func compareZoneBlock(ctx context.Context, leader, shadow *rpc.Client, number uint64) error {
	leaderBlock, err := block(ctx, leader, number)
	if err != nil {
		return err
	}
	shadowBlock, err := block(ctx, shadow, number)
	if err != nil {
		return err
	}
	if leaderBlock.Hash != shadowBlock.Hash || leaderBlock.StateRoot != shadowBlock.StateRoot {
		return fmt.Errorf("Zone follower divergence at block %d: leader hash=%s root=%s shadow hash=%s root=%s", number, leaderBlock.Hash, leaderBlock.StateRoot, shadowBlock.Hash, shadowBlock.StateRoot)
	}
	return nil
}

func countLogs(ctx context.Context, client *rpc.Client, address common.Address, from, to uint64, topics ...common.Hash) (int, error) {
	var logs []logEntry
	filter := map[string]any{
		"address":   address,
		"fromBlock": hexutil.EncodeUint64(from),
		"toBlock":   hexutil.EncodeUint64(to),
		"topics":    []any{topics},
	}
	if err := client.CallContext(ctx, &logs, "eth_getLogs", filter); err != nil {
		return 0, err
	}
	return len(logs), nil
}

func run(ctx context.Context, l1, zone, shadow *rpc.Client, portal common.Address, out *json.Encoder) error {
	l1Start, err := height(ctx, l1)
	if err != nil {
		return err
	}
	zoneStart, err := height(ctx, zone)
	if err != nil {
		return err
	}
	previousL1, previousZone := l1Start, zoneStart
	lastL1Advance, lastZoneAdvance := time.Now(), time.Now()
	var previousShadow uint64
	lastShadowAdvance := time.Now()
	if shadow != nil {
		previousShadow, err = height(ctx, shadow)
		if err != nil {
			return err
		}
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l1Head, err := height(context.Background(), l1)
			if err != nil {
				return err
			}
			zoneHead, err := height(context.Background(), zone)
			if err != nil {
				return err
			}
			deposits, err := countLogs(context.Background(), l1, portal, l1Start, l1Head, depositMade)
			if err != nil {
				return err
			}
			depositTerminals, err := countLogs(context.Background(), zone, common.HexToAddress("0x1c00000000000000000000000000000000000001"), zoneStart, zoneHead, depositProcessed, depositFailed, depositRejected)
			if err != nil {
				return err
			}
			withdrawals, err := countLogs(context.Background(), zone, common.HexToAddress("0x1c00000000000000000000000000000000000002"), zoneStart, zoneHead, withdrawalRequested)
			if err != nil {
				return err
			}
			withdrawalTerminals, err := countLogs(context.Background(), l1, portal, l1Start, l1Head, withdrawalProcessed)
			if err != nil {
				return err
			}
			if deposits == 0 || depositTerminals == 0 || withdrawals == 0 || withdrawalTerminals == 0 {
				return fmt.Errorf("incomplete lifecycle: deposits=%d depositTerminals=%d withdrawals=%d withdrawalTerminals=%d", deposits, depositTerminals, withdrawals, withdrawalTerminals)
			}
			return out.Encode(map[string]any{"event": "passed", "l1Block": l1Head, "zoneBlock": zoneHead, "deposits": deposits, "depositTerminals": depositTerminals, "withdrawals": withdrawals, "withdrawalTerminals": withdrawalTerminals})
		case <-ticker.C:
			l1Head, err := height(ctx, l1)
			if err != nil {
				return err
			}
			zoneHead, err := height(ctx, zone)
			if err != nil {
				return err
			}
			now := time.Now()
			if l1Head > previousL1 {
				lastL1Advance = now
			}
			if zoneHead > previousZone {
				lastZoneAdvance = now
			}
			shadowHead := uint64(0)
			if shadow != nil {
				shadowHead, err = height(ctx, shadow)
				if err != nil {
					return err
				}
				if shadowHead > previousShadow {
					lastShadowAdvance = now
				}
				commonHead := min(zoneHead, shadowHead)
				if commonHead > 0 {
					if err := compareZoneBlock(ctx, zone, shadow, commonHead); err != nil {
						return err
					}
				}
			}
			if now.Sub(lastL1Advance) >= 45*time.Second || now.Sub(lastZoneAdvance) >= 45*time.Second || shadow != nil && now.Sub(lastShadowAdvance) >= 45*time.Second {
				return errors.New("L1 or Zone block production stalled for 45s")
			}
			if err := out.Encode(map[string]any{"event": "health", "l1Block": l1Head, "zoneBlock": zoneHead, "shadowBlock": shadowHead}); err != nil {
				return err
			}
			previousL1, previousZone = l1Head, zoneHead
			previousShadow = shadowHead
		}
	}
}

func main() {
	l1URL := flag.String("l1-rpc", "http://127.0.0.1:28545", "Tempo L1 RPC")
	zoneURL := flag.String("zone-rpc", "http://127.0.0.1:19545", "Zone RPC")
	shadowURL := flag.String("shadow-zone-rpc", "", "optional same-chain Zone shadow RPC")
	portalText := flag.String("portal", "", "Zone Portal address")
	duration := flag.Duration("duration", 4*time.Minute, "campaign duration")
	flag.Parse()
	if !common.IsHexAddress(*portalText) {
		fmt.Fprintln(os.Stderr, "--portal is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()
	l1, err := rpc.DialContext(ctx, *l1URL)
	if err != nil {
		panic(err)
	}
	defer l1.Close()
	zone, err := rpc.DialContext(ctx, *zoneURL)
	if err != nil {
		panic(err)
	}
	defer zone.Close()
	var shadow *rpc.Client
	if *shadowURL != "" {
		shadow, err = rpc.DialContext(ctx, *shadowURL)
		if err != nil {
			panic(err)
		}
		defer shadow.Close()
	}
	if err := run(ctx, l1, zone, shadow, common.HexToAddress(*portalText), json.NewEncoder(os.Stdout)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
