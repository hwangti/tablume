package dbx

import (
	"math/rand"
	"slices"
	"testing"
)

// Placing each column the plan names, in turn, turns the original order
// into the wanted one, and columns already in order are left alone.
func TestPlacements(t *testing.T) {
	apply := func(cur []string, ps []placement) []string {
		cur = slices.Clone(cur)
		for _, p := range ps {
			if i := slices.Index(cur, p.name); i >= 0 {
				cur = slices.Delete(cur, i, i+1)
			}
			at := 0
			if p.after != "" {
				at = slices.Index(cur, p.after) + 1
			}
			cur = slices.Insert(cur, at, p.name)
		}
		return cur
	}
	orig := []string{"a", "b", "c", "d", "e", "f"}
	if ps := placements(orig, []string{"b", "c", "d", "e", "f", "a"}); len(ps) != 1 || ps[0] != (placement{"a", "f"}) {
		t.Errorf("first to last: %v", ps)
	}
	if ps := placements(orig, []string{"f", "a", "b", "c", "d", "e"}); len(ps) != 1 || ps[0] != (placement{"f", ""}) {
		t.Errorf("last to first: %v", ps)
	}
	if ps := placements(orig, orig); len(ps) != 0 {
		t.Errorf("unchanged: %v", ps)
	}
	r := rand.New(rand.NewSource(1))
	for range 500 {
		order := append(slices.Clone(orig), "x", "y")
		r.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		ps := placements(orig, order)
		if got := apply(orig, ps); !slices.Equal(got, order) {
			t.Fatalf("%v -> %v: got %v via %v", orig, order, got, ps)
		}
	}
}

// Only the ON UPDATE expression is taken from EXTRA, not what follows it.
func TestOnUpdateOf(t *testing.T) {
	for extra, want := range map[string]string{
		"":                              "",
		"auto_increment":                "",
		"on update current_timestamp()": "current_timestamp()",
		"on update current_timestamp(), INVISIBLE":                   "current_timestamp()",
		"on update current_timestamp(3) INVISIBLE":                   "current_timestamp(3)",
		"DEFAULT_GENERATED on update CURRENT_TIMESTAMP":              "CURRENT_TIMESTAMP",
		"DEFAULT_GENERATED on update CURRENT_TIMESTAMP(6) INVISIBLE": "CURRENT_TIMESTAMP(6)",
	} {
		if got := onUpdateOf(extra); got != want {
			t.Errorf("onUpdateOf(%q) = %q, want %q", extra, got, want)
		}
	}
}
