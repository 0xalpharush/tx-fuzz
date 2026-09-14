package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"github.com/MariusVanDerWijden/tx-fuzz/tempo"
)

func main() {
	seed := flag.Int64("seed", 1, "reproducible fixture seed")
	chain := flag.Int64("chain-id", 42431, "transaction chain ID")
	flag.Parse()
	cases, err := tempo.Generate(*seed, *chain)
	if err != nil {
		log.Fatal(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	for _, c := range cases {
		if err := encoder.Encode(c); err != nil {
			log.Fatal(err)
		}
	}
}
