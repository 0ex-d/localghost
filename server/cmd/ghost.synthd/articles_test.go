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
	sums := []string{"The minister resigned on Tuesday after a vote of 312 to 290.\n- Her deputy takes over.", "Storms closed 40 schools in the north."}
	s, ok := groundedBrief("- The minister resigned on Tuesday after a 312 to 290 vote.\n• Storms closed 40 schools in the north.", sums)
	if !ok || s != "- The minister resigned on Tuesday after a 312 to 290 vote.\n- Storms closed 40 schools in the north." {
		t.Fatalf("%q %v", s, ok)
	}
	if _, ok := groundedBrief("- The minister resigned after a vote of 312 to 291.\n- Storms closed 40 schools across the north.", sums); ok {
		t.Fatal("a number no story gave")
	}
	if _, ok := groundedBrief("The minister resigned on Tuesday after a 312 to 290 vote, and storms closed 40 schools in the north.", sums); ok {
		t.Fatal("prose, not a point per story")
	}
	if _, ok := groundedBrief("Here are the points:\n- The minister resigned on Tuesday.\n- Storms closed 40 schools in the north.", sums); ok {
		t.Fatal("a preamble")
	}
	if _, ok := groundedBrief("- The minister resigned on Tuesday.\n- Storms closed 40 schools in the north.\n- A third story nobody told about at all.", sums); ok {
		t.Fatal("more points than stories")
	}
	if !sameIDs([]int64{3, 1, 2}, []int64{1, 2, 3}) || sameIDs([]int64{1, 2}, []int64{1, 3}) {
		t.Fatal("same ids")
	}
	p := briefPrompt([]string{newsLead(sums[0]), newsLead(sums[1])})
	if !strings.Contains(p, "2. Storms closed 40 schools") || !strings.Contains(p, "one line per story") || strings.Contains(p, "deputy") {
		t.Fatal(p)
	}
}

func TestLDArticleBody(t *testing.T) {
	body := strings.Repeat("The council voted to keep the library open after a long campaign. ", 12)
	page := `<html><head><script type="application/ld+json">{"@context":"https://schema.org","@graph":[{"@type":"WebPage"},{"@type":"NewsArticle","articleBody":"` + body + `\nA second paragraph of the article."}]}</script></head><body><div id="app"></div></body></html>`
	text, pw := articleText(page)
	if pw || !strings.HasPrefix(text, "The council voted") || !strings.HasSuffix(text, "A second paragraph of the article.") {
		t.Fatalf("%v %q", pw, text)
	}
	paid := `<script type="application/ld+json">{"@type":"NewsArticle","isAccessibleForFree":false,"articleBody":"` + body + `"}</script><article><p>The free part of the article, long enough to count as prose here.</p></article>`
	if text, pw := articleText(paid); !pw || strings.Contains(text, "library") {
		t.Fatalf("a paywalled page keeps its free part only: %v %q", pw, text)
	}
}
