package hw

import "testing"

// The phone shows the key rows and folds the rest; a summary that marks nothing (an older daemon,
// the default case) still shows something.
func TestKeyRows(t *testing.T) {
	kv := []DaemonKV{{K: "a", V: "1"}, {K: "b", V: "2", Key: true}, {K: "c", V: "3"}, {K: "d", V: "4", Key: true}}
	got := KeyRows(kv, 3)
	if len(got) != 2 || got[0].K != "b" || got[1].K != "d" {
		t.Fatalf("key rows: %+v", got)
	}
	plain := []DaemonKV{{K: "a", V: "1"}, {K: "b", V: "2"}, {K: "c", V: "3"}}
	if got := KeyRows(plain, 2); len(got) != 2 || got[1].K != "b" {
		t.Fatalf("unmarked rows fall back to the first n: %+v", got)
	}
	if got := KeyRows(plain, 10); len(got) != 3 {
		t.Fatalf("n past the end: %+v", got)
	}
	if got := KeyRows(nil, 3); len(got) != 0 {
		t.Fatalf("empty: %+v", got)
	}
}
