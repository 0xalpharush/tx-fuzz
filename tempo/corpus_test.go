package tempo

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tempoxyz/tempo-go/pkg/precompiles"
	"github.com/tempoxyz/tempo-go/pkg/transaction"
)

func TestCorpusCoverageAndReplay(t *testing.T) {
	for _, seed := range []int64{0, 1, 42, 9223372036854775807} {
		cases, err := Generate(seed, 42431)
		require.NoError(t, err)
		again, err := Generate(seed, 42431)
		require.NoError(t, err)
		require.Equal(t, cases, again)
		seen := map[string]bool{}
		for _, c := range cases {
			tx, err := transaction.Deserialize(c.Raw)
			require.NoError(t, err)
			raw, err := transaction.Serialize(tx, nil)
			require.NoError(t, err)
			require.Equal(t, c.Raw, raw)
			if c.Family != "keychain" {
				_, err = transaction.VerifySignature(tx)
				require.NoError(t, err, c.Family)
			}
			if c.Interface != "" {
				contract, err := precompiles.ABI(c.Interface)
				require.NoError(t, err)
				method, err := contract.MethodById(tx.Calls[0].Data[:4])
				require.NoError(t, err)
				require.Equal(t, c.Method, method.Sig)
				args, err := method.Inputs.Unpack(tx.Calls[0].Data[4:])
				require.NoError(t, err)
				encoded, err := method.Inputs.Pack(args...)
				require.NoError(t, err)
				require.Equal(t, hex.EncodeToString(tx.Calls[0].Data[4:]), hex.EncodeToString(encoded))
				seen[c.Interface+"."+c.Method] = true
			}
		}
		expected := 0
		for _, name := range precompiles.Names() {
			contract, err := precompiles.ABI(name)
			require.NoError(t, err)
			expected += len(contract.Methods)
		}
		require.Len(t, seen, expected)
		t.Logf("seed %d: %d ABI methods and %d transaction features", seed, len(seen), len(cases)-len(seen))
	}
}
