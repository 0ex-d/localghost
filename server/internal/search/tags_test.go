package search

import "testing"

func TestParseTagsWithCategories(t *testing.T) {
	got := ParseTags("place:beach, People:child, food: ice cream, swimming, weird:thing, x, sunset, beach")
	want := []Tag{{"beach", "place"}, {"child", "people"}, {"ice cream", "food"}, {"swimming", "activity"},
		{"weird:thing", ""}, {"sunset", "nature"}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tag %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLexiconCompoundsAndPlurals(t *testing.T) {
	cases := map[string]string{
		"dog": "animal", "dogs": "animal", "wooden table": "object", "red car": "vehicle",
		"fishing boat": "vehicle", "birthday cake": "food", "mountains": "nature", "pastel de nata": "food",
		"quantum foam": "", "beach": "place", "picnic": "activity",
	}
	for tag, want := range cases {
		if got := Lexicon(tag); got != want {
			t.Errorf("Lexicon(%q) = %q, want %q", tag, got, want)
		}
	}
	for _, c := range Categories {
		if !IsCategory(c) {
			t.Fatal(c)
		}
	}
	if IsCategory("other") {
		t.Fatal("'other' is a display bucket, not a category")
	}
}

func TestSortedCategories(t *testing.T) {
	m := map[string][]string{"other": {"x"}, "food": {"a"}, "people": {"b"}, "place": {"c"}}
	got := SortedCategories(m)
	if got[0] != "people" || got[1] != "place" || got[2] != "food" || got[3] != "other" {
		t.Fatalf("%v", got)
	}
}

func TestAssignCategoriesReadsWhatAModelWrites(t *testing.T) {
	asked := []string{"beach", "fishing boats", "ice-cream", "zorblax"}
	cases := []struct {
		name, raw string
		want      []string // categories in asked order
	}{
		{"one line, as asked", "place:beach, vehicle:fishing boats, food:ice-cream, other:zorblax",
			[]string{"place", "vehicle", "food", "other"}},
		// one pair per line, numbered , a comma split read this as one bad tag and placed nothing
		{"numbered lines", "1. place:beach\n2. vehicle:fishing boat\n3. food:ice cream\n4. other:zorblax",
			[]string{"place", "vehicle", "food", "other"}},
		{"bold list, other way round", "- **beach**: place\n- **fishing boats**: vehicle\n- **ice-cream**: food",
			[]string{"place", "vehicle", "food", "other"}},
		{"parentheses", "beach (place), fishing boats (vehicle), ice cream (food), zorblax (other)",
			[]string{"place", "vehicle", "food", "other"}},
		// the model rewrote the names but kept one pair per tag, in order: by position
		{"renamed, same order", "place:sandy beach, vehicle:trawlers, food:gelato, object:blob",
			[]string{"place", "vehicle", "food", "object"}},
		// a tag left out is a verdict, not a retry
		{"one left out", "place:beach, vehicle:fishing boats, food:ice-cream",
			[]string{"place", "vehicle", "food", "other"}},
		{"invented category", "place:beach, vehicle:fishing boats, food:ice-cream, alien:zorblax",
			[]string{"place", "vehicle", "food", "other"}},
	}
	for _, c := range cases {
		got, ok := AssignCategories(asked, c.raw)
		if !ok || len(got) != len(asked) {
			t.Fatalf("%s: ok=%v got %v", c.name, ok, got)
		}
		for i, w := range c.want {
			if got[i].Name != asked[i] || got[i].Category != w {
				t.Errorf("%s: %q -> %+v, want %s", c.name, asked[i], got[i], w)
			}
		}
	}
	// an answer with no pair in it is a failed call, retried, never four "other"s
	for _, raw := range []string{"", "I cannot see the photo.", "beach, boats, ice cream, zorblax"} {
		if got, ok := AssignCategories(asked, raw); ok {
			t.Fatalf("%q accepted: %v", raw, got)
		}
	}
}

func TestParseTagsReadsNumberedLines(t *testing.T) {
	got := ParseTags("1. place:beach\n2. **people:child**\n- food:ice cream")
	if len(got) != 3 || got[0] != (Tag{"beach", "place"}) || got[1] != (Tag{"child", "people"}) || got[2] != (Tag{"ice cream", "food"}) {
		t.Fatalf("%v", got)
	}
}
