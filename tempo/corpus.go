// Package tempo generates bounded, reproducible offline Tempo SDK fixtures.
// It performs no RPC calls and makes no claim about stateful execution success.
package tempo

import (
	"fmt"
	"math/big"
	"math/rand"
	"reflect"
	"sort"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tempoxyz/tempo-go/pkg/keychain"
	"github.com/tempoxyz/tempo-go/pkg/precompiles"
	"github.com/tempoxyz/tempo-go/pkg/signer"
	"github.com/tempoxyz/tempo-go/pkg/transaction"
)

// Case records the intended coverage and a serialized Tempo transaction.
// The included signatures use public, deterministically derived fixture keys.
type Case struct {
	Family    string `json:"family"`
	Interface string `json:"interface,omitempty"`
	Method    string `json:"method,omitempty"`
	Seed      int64  `json:"seed"`
	Raw       string `json:"raw"`
}

// Generate emits one case per ABI method and additional transaction features.
// Dynamic deployment targets are fixture addresses. Call arguments are ABI-valid
// samples, not a stateful execution sequence; permissions/balances are not set up.
func Generate(seed int64, chainID int64) ([]Case, error) {
	if chainID <= 0 {
		return nil, fmt.Errorf("chain ID must be positive")
	}
	r := rand.New(rand.NewSource(seed))
	key, err := crypto.ToECDSA(crypto.Keccak256([]byte(fmt.Sprintf("tx-fuzz offline fixture %d", seed))))
	if err != nil {
		return nil, err
	}
	s := signer.NewSignerFromKey(key)
	target := common.HexToAddress("0x1111111111111111111111111111111111111111")
	base := func() *transaction.Tx {
		tx := transaction.NewDefault(chainID)
		tx.Gas = 1000000
		tx.MaxFeePerGas.SetUint64(20000000000)
		tx.Calls = []transaction.Call{{To: &target, Value: new(big.Int)}}
		return tx
	}
	var cases []Case
	add := func(family, name, method string, tx *transaction.Tx) error {
		if tx.Signature == nil {
			if err := transaction.SignTransaction(tx, s); err != nil {
				return err
			}
		}
		raw, err := transaction.Serialize(tx, nil)
		if err != nil {
			return err
		}
		cases = append(cases, Case{Family: family, Interface: name, Method: method, Seed: seed, Raw: raw})
		return nil
	}
	for _, name := range precompiles.Names() {
		contract, err := precompiles.ABI(name)
		if err != nil {
			return nil, err
		}
		var names []string
		for name := range contract.Methods {
			names = append(names, name)
		}
		sort.Strings(names)
		address, fixed := precompiles.Address(name)
		if !fixed {
			address = target
		}
		for _, methodName := range names {
			method := contract.Methods[methodName]
			args := make([]interface{}, len(method.Inputs))
			for i, arg := range method.Inputs {
				args[i] = value(arg.Type, r).Interface()
			}
			call, err := precompiles.Call(name, address, methodName, args...)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", name, method.Sig, err)
			}
			tx := base()
			tx.Calls = []transaction.Call{call}
			if err := add("precompile", name, method.Sig, tx); err != nil {
				return nil, err
			}
		}
	}
	features := []string{"batch", "create", "access-list", "parallel-nonce", "expiring-nonce", "validity", "fee-token", "sponsored", "delegation", "keychain", "inline-key"}
	for _, feature := range features {
		tx := base()
		switch feature {
		case "batch":
			tx.Calls = append(tx.Calls, tx.Calls[0])
		case "create":
			tx.Calls[0].To = nil
			tx.Calls[0].Data = []byte{0x60, 0, 0x60, 0, 0xf3}
		case "access-list":
			tx.AccessList = transaction.AccessList{{Address: target, StorageKeys: []common.Hash{{}}}}
		case "parallel-nonce":
			tx.NonceKey.SetUint64(42)
		case "expiring-nonce":
			tx.NonceKey.Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
			tx.ValidBefore = 2000000000
		case "validity":
			tx.ValidAfter = 1900000000
			tx.ValidBefore = 2000000000
		case "fee-token":
			tx.FeeToken = transaction.AlphaUSDAddress
		case "sponsored":
			tx.AwaitingFeePayer = true
			if err := transaction.SignTransaction(tx, s); err != nil {
				return nil, err
			}
			if err := transaction.AddFeePayerSignature(tx, s); err != nil {
				return nil, err
			}
		case "delegation":
			auth := transaction.SignedAuthorization{ChainID: big.NewInt(chainID), Address: target}
			if err := auth.Sign(s); err != nil {
				return nil, err
			}
			tx.AuthorizationList = []transaction.SignedAuthorization{auth}
		case "keychain":
			if err := keychain.SignWithAccessKey(tx, s, target); err != nil {
				return nil, err
			}
		case "inline-key":
			auth := keychain.NewKeyAuthorization(uint64(chainID), 0, target).WithExpiry(2000000000).WithNoSpending().WithNoCalls()
			if err := auth.SignAndAttach(tx, s); err != nil {
				return nil, err
			}
		}
		if err := add(feature, "", "", tx); err != nil {
			return nil, err
		}
	}
	return cases, nil
}

func value(t abi.Type, r *rand.Rand) reflect.Value {
	v := reflect.New(t.GetType()).Elem()
	switch t.T {
	case abi.IntTy, abi.UintTy:
		if t.Size > 64 {
			n := new(big.Int).SetUint64(r.Uint64())
			if t.T == abi.IntTy && r.Intn(2) == 0 {
				n.Neg(n)
			}
			v.Set(reflect.ValueOf(n))
		} else if t.T == abi.IntTy {
			v.SetInt(int64(r.Uint64()))
		} else {
			v.SetUint(r.Uint64())
		}
	case abi.BoolTy:
		v.SetBool(r.Intn(2) == 0)
	case abi.StringTy:
		v.SetString(fmt.Sprintf("fixture-%x", r.Uint64()))
	case abi.BytesTy:
		b := make([]byte, r.Intn(33))
		_, _ = r.Read(b)
		v.SetBytes(b)
	case abi.AddressTy, abi.FixedBytesTy, abi.FunctionTy:
		for i := 0; i < v.Len(); i++ {
			v.Index(i).SetUint(uint64(r.Intn(256)))
		}
	case abi.SliceTy:
		v = reflect.MakeSlice(t.GetType(), r.Intn(3), 2)
		for i := 0; i < v.Len(); i++ {
			v.Index(i).Set(value(*t.Elem, r))
		}
	case abi.ArrayTy:
		for i := 0; i < v.Len(); i++ {
			v.Index(i).Set(value(*t.Elem, r))
		}
	case abi.TupleTy:
		for i, elem := range t.TupleElems {
			v.Field(i).Set(value(*elem, r))
		}
	default:
		panic(fmt.Sprintf("unsupported ABI type %s", t.String()))
	}
	return v
}
