package main

import "testing"

func TestParseLocalEndpoint(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:8545", "http://[::1]:8545"} {
		if _, err := parseEndpoint(endpoint, "baseline"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := parseEndpoint("http://baseline:8545", "baseline"); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"https://127.0.0.1:8545", "http://localhost:8545", "http://10.0.0.1:8545", "http://user@127.0.0.1:8545", ""} {
		if _, err := parseEndpoint(endpoint, "baseline"); err == nil {
			t.Fatalf("accepted %q", endpoint)
		}
	}
	if _, err := parseEndpoint("http://candidate:8545", "baseline"); err == nil {
		t.Fatal("accepted the wrong enclave service")
	}
}

func TestEqualExecutionModelsOutcomeClasses(t *testing.T) {
	if !equalExecution(execution{Class: outcomeRejected, Error: "code=-1"}, execution{Class: outcomeRejected, Error: "code=-2"}) {
		t.Fatal("two rejected inputs should have the same state-transition class")
	}
	if equalExecution(execution{Class: outcomeRejected}, execution{Class: outcomeReverted}) {
		t.Fatal("rejected and reverted must remain distinct")
	}
	a := execution{Class: outcomeAccepted, Receipt: &receiptResult{Status: 1, GasUsed: 21_000, Logs: []logEntry{}}}
	b := execution{Class: outcomeAccepted, Receipt: &receiptResult{Status: 1, GasUsed: 21_001, Logs: []logEntry{}}}
	if equalExecution(a, b) {
		t.Fatal("gas divergence was ignored")
	}
}
