package main

import (
	"strings"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

var testKnown = map[string]string{"BTC": "BTC", "ETH": "ETH", "LINK": "LINK", "NEAR": "NEAR", "AVAX": "AVAX", "SKY": "SKY",
	"chainlink": "LINK", "avalanche": "AVAX", "near protocol": "NEAR", "bitcoin": "BTC", "ethereum": "ETH"}

// What the box keeps is not searched for: a coin's price by symbol or name, the top coins, crypto
// as a whole, a rate between currencies, the day's headlines. Everything else is the web's.
func TestBoxHas(t *testing.T) {
	for _, q := range []string{
		"what's the btc price?", "ETH", "how is LINK doing today", "chainlink price", "avalanche worth now?",
		"top 50 cryptos", "show me the biggest coins", "how is crypto doing today?",
		"100 euros in pounds", "gbp to ron exchange rate", "what's the news", "today's headlines", "news summary",
	} {
		if _, ok := boxHas(q, testKnown); !ok {
			t.Errorf("the box has %q", q)
		}
	}
	for _, q := range []string{
		"where is the nearest pharmacy", "link me the article about the strike", "the sky was red tonight",
		"news about the rail strike in France", "who won the match yesterday", "is bitcoin a good long-term idea for a pension fund in theory",
	} {
		if why, ok := boxHas(q, testKnown); ok {
			t.Errorf("the box does not have %q (%s)", q, why)
		}
	}
	if c := coinsIn("compare NEAR and chainlink, and near the end LINK again", testKnown); strings.Join(c, ",") != "NEAR,LINK" {
		t.Fatalf("coins: %v", c)
	}
	if topCount("top 50 coins") != 50 || topCount("biggest cryptos") != 10 || topCount("top 200 coins") != 50 {
		t.Fatal("top count")
	}
}

func TestTopItem(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	snap := hw.RatesSnapshot{Source: "coinbase-ranks", RanksAt: now.Add(-20 * time.Minute).Unix(),
		Ranks: []hw.CoinRow{{Rank: 1, Symbol: "BTC", Name: "Bitcoin", PriceUSD: 84000, MarketCap: 1.69e12, Change24: 0.9},
			{Rank: 2, Symbol: "ETH", Name: "Ethereum", PriceUSD: 2690, MarketCap: 3.2e11, Change24: -1.2}},
		Index: map[string]hw.IndexRow{"BTC": {Price: 84497.6}}}
	it, ok := topItem(snap, 50, now)
	if !ok || !strings.Contains(it.Snippet, "the 2 largest coins by market cap (Coinbase's list, 20m0s old)") ||
		!strings.Contains(it.Snippet, "1. BTC (Bitcoin) 84,498 USD, cap 1.69T, +0.9% 24 h") || !strings.Contains(it.Snippet, "2. ETH (Ethereum) 2,690 USD, cap 320.0B") {
		t.Fatalf("%+v", it)
	}
	if _, ok := topItem(hw.RatesSnapshot{}, 10, now); ok {
		t.Fatal("no list, no item")
	}
}
