package rates

import (
	"math"
	"testing"
)

func TestWeightsAndValue(t *testing.T) {
	avg := map[string]float64{"BTC": 3e10, "ETH": 1.5e10, "SOL": 3e9, "USDT": 1e11, "WBTC": 1e9, "DOGE": 2e9, "NEW": 5e9}
	caps := map[string]float64{"BTC": 1.3e12, "ETH": 4e11, "SOL": 7e10, "USDT": 1.7e11, "WBTC": 1e10, "DOGE": 1.8e10, "NEW": 9e10}
	base := map[string]float64{"BTC": 65000, "ETH": 3500, "SOL": 150, "USDT": 1, "WBTC": 65000, "DOGE": 0.12} // NEW has no price yet
	cons := Weights(avg, caps, base, 3)
	if len(cons) != 3 || cons[0].Symbol != "BTC" || cons[1].Symbol != "ETH" || cons[2].Symbol != "SOL" {
		t.Fatalf("constituents: %+v", cons)
	}
	total := 3e10 + 1.5e10 + 3e9
	if math.Abs(cons[0].Weight-3e10/total) > 1e-12 || math.Abs(cons[0].Weight+cons[1].Weight+cons[2].Weight-1) > 1e-12 || cons[2].Rank != 3 {
		t.Fatalf("weights: %+v", cons)
	}
	// flat prices: the chain value stands
	v, priced, missing := Value(1000, cons, map[string]float64{"BTC": 65000, "ETH": 3500, "SOL": 150}, nil)
	if math.Abs(v-1000) > 1e-9 || priced != 3 || len(missing) != 0 {
		t.Fatalf("flat: %v %d %v", v, priced, missing)
	}
	// BTC up 10%, the others flat: up by BTC's weight times 10%
	v, _, _ = Value(1000, cons, map[string]float64{"BTC": 71500, "ETH": 3500, "SOL": 150}, nil)
	if math.Abs(v-1000*(1+0.1*cons[0].Weight)) > 1e-9 {
		t.Fatalf("btc up: %v", v)
	}
	// SOL has no price today: carried at yesterday's, named
	v, priced, missing = Value(1000, cons, map[string]float64{"BTC": 65000, "ETH": 3500}, map[string]float64{"SOL": 165})
	if priced != 2 || len(missing) != 1 || missing[0] != "SOL" || math.Abs(v-1000*(1+0.1*cons[2].Weight)) > 1e-9 {
		t.Fatalf("carried: %v %d %v", v, priced, missing)
	}
	// nothing carried either: flat at the base
	v, _, _ = Value(1000, cons, map[string]float64{"BTC": 65000, "ETH": 3500}, nil)
	if math.Abs(v-1000) > 1e-9 {
		t.Fatalf("base: %v", v)
	}
	if v, _, _ := Value(0, cons, nil, nil); v != 0 {
		t.Fatal("no chain")
	}
	if MonthOf("2026-10-01") != "2026-10" {
		t.Fatal(MonthOf("2026-10-01"))
	}
}
