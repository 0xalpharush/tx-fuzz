package tempo

import (
	"bytes"
	"testing"
)

func TestGenerateProgramsDeterministic(t *testing.T) {
	a, err := GeneratePrograms(7, 64, 512)
	if err != nil {
		t.Fatal(err)
	}
	b, err := GeneratePrograms(7, 64, 512)
	if err != nil {
		t.Fatal(err)
	}
	for i := range a {
		if a[i].ID != b[i].ID || !bytes.Equal(a[i].Runtime, b[i].Runtime) || !bytes.Equal(a[i].Calldata, b[i].Calldata) {
			t.Fatalf("program %d is not reproducible", i)
		}
	}
}

func TestRuntimeInitCode(t *testing.T) {
	runtime := []byte{0x60, 0x2a, 0x60, 0, 0x52, 0x60, 0x20, 0x60, 0, 0xf3}
	init, err := RuntimeInitCode(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(init) != len(runtime)+15 || !bytes.Equal(init[15:], runtime) {
		t.Fatalf("bad initcode: %x", init)
	}
}
