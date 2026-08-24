package domain

import "sort"

// SortReasons returns a new, deterministically sorted copy of the supplied
// reasons. Identical conflicts must produce identical ordered reason sets so
// that audit text and idempotency digests are stable across runs and
// restarts.
func SortReasons(reasons ...string) []string {
	out := make([]string, len(reasons))
	copy(out, reasons)
	sort.Strings(out)
	return out
}

// JoinReasons merges several reason slices, deduplicates them and returns a
// single deterministically sorted slice. It is used when a single rejection
// aggregates causes from several independent checks (e.g. a triple-split
// matrix failure covering multiple blind codes).
func JoinReasons(groups ...[]string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, g := range groups {
		for _, r := range g {
			if r == "" || seen[r] {
				continue
			}
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}
