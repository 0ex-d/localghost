package main

import (
	"strings"
	"testing"
)

// The article's own paragraphs, without the page around them; a page that says it is not free is
// marked so, and only what it serves is kept.
func TestArticleText(t *testing.T) {
	page := `<html><head><script>var x = "<p>not this</p>";</script>
		<script type="application/ld+json">{"@type":"NewsArticle","isAccessibleForFree": "False"}</script></head>
		<body><nav><p>Home, World, Business, Sport and everything else in the menu</p></nav>
		<article><h1>Minister resigns</h1>
		<p>The minister resigned on Tuesday after <a href="/x">a vote</a> of 312 to 290 in the chamber, ending a week of talks.</p>
		<p>Subscribe to our newsletter for more.</p>
		<p>Her deputy, Ana Pop, takes over until a new minister is named, the government said in a statement.</p>
		<p>Short.</p>
		<p>The minister resigned on Tuesday after <a href="/x">a vote</a> of 312 to 290 in the chamber, ending a week of talks.</p>
		<footer><p>All rights reserved, the newspaper company, its owners and its many lawyers.</p></footer>
		</article></body></html>`
	text, paywalled := articleText(page)
	if !paywalled {
		t.Fatal("the page says it is not free")
	}
	lines := strings.Split(text, "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "The minister resigned on Tuesday after a vote of 312 to 290") || !strings.HasPrefix(lines[1], "Her deputy, Ana Pop") {
		t.Fatalf("%q", text)
	}
	if _, pw := articleText(`<main><p>Nothing about access here, a long enough paragraph to count as prose.</p></main>`); pw {
		t.Fatal("no paywall said, none assumed")
	}
}

func TestGroundedBrief(t *testing.T) {
	sums := []string{"The minister resigned on Tuesday after a vote of 312 to 290.", "Storms closed 40 schools in the north."}
	if s, ok := groundedBrief("The minister resigned on Tuesday after a 312 to 290 vote, and storms closed 40 schools in the north.", sums); !ok || !strings.HasPrefix(s, "The minister") {
		t.Fatalf("%q %v", s, ok)
	}
	if _, ok := groundedBrief("The minister resigned after a vote of 312 to 291, and storms closed 40 schools across the north.", sums); ok {
		t.Fatal("a number no story gave")
	}
	if _, ok := groundedBrief("- minister resigned\n- storms closed 40 schools in the north today", sums); ok {
		t.Fatal("a list")
	}
	if !sameIDs([]int64{3, 1, 2}, []int64{1, 2, 3}) || sameIDs([]int64{1, 2}, []int64{1, 3}) {
		t.Fatal("same ids")
	}
	if p := briefPrompt(sums); !strings.Contains(p, "- Storms closed 40 schools") || !strings.Contains(p, "three or four plain sentences") {
		t.Fatal(p)
	}
}
