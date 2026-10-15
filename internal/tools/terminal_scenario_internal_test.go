package tools

import (
	"testing"
	"time"
)

func TestTerminalCreateKeyIsBucketedByTime(t *testing.T) {
	owned := 10.0
	args := setTerminalScenarioArgs{Ticker: "TSLA", SharesOwned: &owned}
	base := time.Unix(1_700_000_100, 0) // bucket start is a multiple of 300

	same := terminalCreateKey("u1", args, base)
	if got := terminalCreateKey("u1", args, base.Add(299*time.Second-100*time.Second)); got != same {
		t.Errorf("same bucket gave a different key: %s vs %s", got, same)
	}
	if got := terminalCreateKey("u1", args, base.Add(10*time.Minute)); got == same {
		t.Errorf("a later bucket reused the key %s: a deliberate re-create would be replayed", got)
	}
}
