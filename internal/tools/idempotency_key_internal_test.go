package tools

import (
	"testing"
	"time"
)

func TestIdempotencyKeyRepeatsWithinAWindowOnly(t *testing.T) {
	type args struct{ Title string }
	base := time.Unix(1_800_000_000, 0) // start of a 5-minute bucket
	first := idempotencyKeyAt("u1", args{"coffee"}, base)

	if got := idempotencyKeyAt("u1", args{"coffee"}, base.Add(299*time.Second)); got != first {
		t.Errorf("retry inside the window got a new key: %q != %q", got, first)
	}
	if got := idempotencyKeyAt("u1", args{"coffee"}, base.Add(300*time.Second)); got == first {
		t.Errorf("identical write in the next window kept the key %q, so the backend would replay it and write nothing", first)
	}
	if got := idempotencyKeyAt("u2", args{"coffee"}, base); got == first {
		t.Error("two users share a key")
	}
	if got := idempotencyKeyAt("u1", args{"tea"}, base); got == first {
		t.Error("different arguments share a key")
	}
}
