package search

import (
	"strings"
	"testing"
)

func TestNormalizeCaptionPutsTheHeadingsBackAndRefusesTheRest(t *testing.T) {
	// the model's markdown dress: bold headings, a title line, a colour spelling
	raw := "Here is the description:\n\n**Scene:** A harbour at dusk, boats moored along a stone quay, warm light.\n**Objects:** boats, quay, ropes\n**People:** none\n**Text:** none\n**Colors_Style:** amber and navy\n## Setting guess: a Greek island harbour, likely"
	out, ok := NormalizeCaption(raw)
	if !ok {
		t.Fatal("a caption with sections refused")
	}
	if !strings.HasPrefix(out, "SCENE: A harbour at dusk") {
		t.Fatalf("preamble or heading wrong: %q", out)
	}
	for _, h := range []string{"\nOBJECTS: boats", "\nPEOPLE: none", "\nTEXT: none", "\nCOLOURS_STYLE: amber", "\nSETTING_GUESS: a Greek"} {
		if !strings.Contains(out, h) {
			t.Fatalf("heading %q not normalised in:\n%s", h, out)
		}
	}
	if strings.Contains(out, "**") || strings.Contains(out, "##") || strings.Contains(out, "Here is") {
		t.Fatalf("dressing left in: %q", out)
	}
	// the section reader works on the normalised text
	if sc := captionSection(out, "SCENE:"); !strings.HasPrefix(sc, "A harbour at dusk") || strings.Contains(sc, "boats, quay") {
		t.Fatalf("scene section: %q", sc)
	}
	// already in the contract's form: unchanged
	clean := "SCENE: A beach.\nOBJECTS: sand\nPEOPLE: none\nTEXT: none\nCOLOURS_STYLE: blue\nSETTING_GUESS: a beach"
	if out2, ok := NormalizeCaption(clean); !ok || out2 != clean {
		t.Fatalf("clean caption changed: %q", out2)
	}
	// thinking that ran out of budget, a refusal, prose: no SCENE, no caption
	for _, bad := range []string{
		"Okay, the user wants me to describe this image for a search index. Let me look at it carefully. I can see what appears to be a harbour with several boats...",
		"I'm sorry, but I can't help with describing this image.",
		"A harbour at dusk with boats along the quay. Objects: boats. People: none.",
		"SCENE:\nOBJECTS: x",
	} {
		if _, ok := NormalizeCaption(bad); ok {
			t.Fatalf("accepted as a caption: %q", bad)
		}
	}
}
