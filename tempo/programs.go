package tempo

import (
	"encoding/hex"
	"fmt"
	"math/rand"

	"github.com/MariusVanDerWijden/FuzzyVM/filler"
	"github.com/MariusVanDerWijden/FuzzyVM/generator"
	"github.com/ethereum/go-ethereum/crypto"
)

// Program is one deterministic EVM differential-test input.
type Program struct {
	Index    int
	ID       string
	Runtime  []byte
	Calldata []byte
}

// GeneratePrograms uses tx-fuzz's FuzzyVM generator. Both engines execute the
// same canonical block, so environment opcodes are useful differential inputs.
func GeneratePrograms(seed int64, count, maxCodeBytes int) ([]Program, error) {
	if count < 0 {
		return nil, fmt.Errorf("program count must not be negative")
	}
	if maxCodeBytes < 1 || maxCodeBytes > 0xffff {
		return nil, fmt.Errorf("max code bytes must be in [1, 65535]")
	}
	rng := rand.New(rand.NewSource(seed))
	programs := make([]Program, 0, count)
	for attempts := 0; len(programs) < count && attempts < count*100+100; attempts++ {
		entropy := make([]byte, 10_000)
		if _, err := rng.Read(entropy); err != nil {
			return nil, err
		}
		_, runtime := generator.GenerateProgram(filler.NewFiller(entropy))
		if len(runtime) == 0 {
			continue
		}
		if len(runtime) > maxCodeBytes {
			runtime = runtime[:maxCodeBytes]
		}
		calldata := make([]byte, rng.Intn(129))
		if _, err := rng.Read(calldata); err != nil {
			return nil, err
		}
		digest := crypto.Keccak256(runtime, calldata)
		programs = append(programs, Program{
			Index:    len(programs),
			ID:       hex.EncodeToString(digest[:8]),
			Runtime:  append([]byte(nil), runtime...),
			Calldata: calldata,
		})
	}
	if len(programs) != count {
		return nil, fmt.Errorf("generated %d of %d programs", len(programs), count)
	}
	return programs, nil
}

// RuntimeInitCode returns initcode that installs runtime without executing it.
func RuntimeInitCode(runtime []byte) ([]byte, error) {
	if len(runtime) == 0 || len(runtime) > 0xffff {
		return nil, fmt.Errorf("runtime length must be in [1, 65535]")
	}
	hi, lo := byte(len(runtime)>>8), byte(len(runtime))
	// PUSH2 size; PUSH2 15; PUSH1 0; CODECOPY; PUSH2 size; PUSH1 0; RETURN.
	init := []byte{0x61, hi, lo, 0x61, 0x00, 0x0f, 0x60, 0x00, 0x39, 0x61, hi, lo, 0x60, 0x00, 0xf3}
	return append(init, runtime...), nil
}
