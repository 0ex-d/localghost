package main

import (
	"strings"
	"testing"
)

func TestPickWikiTitle(t *testing.T) {
	body := `{"query":{"search":[
		{"title":"List of cryptocurrencies","snippet":"Solana is a <span>cryptocurrency</span>"},
		{"title":"Solana (company)","snippet":"a maker of software"},
		{"title":"Solana (blockchain platform)","snippet":"<span class=\"searchmatch\">Solana</span> is a blockchain platform"}]}}`
	if got := pickWikiTitle(body, "Solana", "SOL"); got != "Solana (blockchain platform)" {
		t.Fatalf("%q", got)
	}
	if got := pickWikiTitle(`{"query":{"search":[{"title":"Bitcoin","snippet":"Bitcoin is a cryptocurrency"}]}}`, "Wrapped Bitcoin", "WBTC"); got != "" {
		t.Fatalf("another coin's article: %q", got)
	}
	if pickWikiTitle("not json", "Solana", "SOL") != "" {
		t.Fatal("unreadable search")
	}
}

func TestParseWikiSummary(t *testing.T) {
	ok := `{"type":"standard","extract":"Solana is a blockchain platform which uses a proof-of-stake mechanism to provide smart contract functionality. Its native cryptocurrency is SOL. It was launched in 2020."}`
	if got := parseWikiSummary(ok, "Solana", "SOL"); !strings.HasPrefix(got, "Solana is a blockchain") {
		t.Fatalf("%q", got)
	}
	if parseWikiSummary(`{"type":"disambiguation","extract":"Solana may refer to: a blockchain, a coin, a town in the hills of somewhere far away."}`, "Solana", "SOL") != "" {
		t.Fatal("a disambiguation page is not the coin")
	}
	if parseWikiSummary(`{"type":"standard","extract":"Solana Beach is a coastal city in San Diego County, California, United States, with a long history."}`, "Solana", "SOL") != "" {
		t.Fatal("not about a cryptocurrency")
	}
}

func TestSiteText(t *testing.T) {
	page := `<html><head><meta content="Fast, low-cost payments for everyone." name="description">
		<meta property='og:description' content='Solana is a decentralised network for apps, with fees under a cent.'></head>
		<body><main><p>Build on the network that more than 1,000 projects already use for payments and apps.</p>
		<p>Cookie settings</p></main></body></html>`
	got := siteText(page)
	if !strings.HasPrefix(got, "Solana is a decentralised network") || !strings.Contains(got, "1,000 projects") || strings.Contains(got, "Cookie") {
		t.Fatalf("%q", got)
	}
}

func TestPublicSite(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.solana.com/": "solana.com",
		"http://bitcoin.org":      "bitcoin.org",
		"https://192.168.1.10/":   "",
		"https://localhost:8080":  "",
		"ftp://example.org":       "",
		"https://box.local/":      "",
		"https://user:pw@x.org/":  "",
		"":                        "",
	} {
		got, ok := publicSite(in)
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
}

func TestCoinDescPromptAndGrounding(t *testing.T) {
	src := []coinSource{
		{From: "Wikipedia", Text: "Solana is a blockchain platform which uses proof-of-stake. It was launched in 2020 by Anatoly Yakovenko."},
		{From: "Coinbase", Text: "Solana is a fast network for decentralised apps."},
	}
	p := coinDescPrompt("Solana", "SOL", src)
	if !strings.Contains(p, "Solana (SOL)") || !strings.Contains(p, "[Wikipedia]\nSolana is") || !strings.Contains(p, "No price") {
		t.Fatal(p)
	}
	good := "Solana is a blockchain platform for decentralised apps that uses proof-of-stake. It was launched in 2020 by Anatoly Yakovenko."
	if got, ok := groundedCoinText(good, "Solana", "SOL", src); !ok || got != good {
		t.Fatalf("%v %q", ok, got)
	}
	if _, ok := groundedCoinText("Solana is a blockchain platform launched in 2019 by Anatoly Yakovenko, for decentralised apps.", "Solana", "SOL", src); ok {
		t.Fatal("a year the sources do not give")
	}
	if _, ok := groundedCoinText("Solana is a blockchain platform for decentralised apps, and you should buy it before it grows.", "Solana", "SOL", src); ok {
		t.Fatal("advice")
	}
	if _, ok := groundedCoinText("It is a blockchain platform for decentralised apps that uses proof-of-stake to agree.", "Solana", "SOL", src); ok {
		t.Fatal("the coin is not named")
	}
}
