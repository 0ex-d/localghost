package feedstat

import "testing"

// Every source id lands in its kind, and every kind's ids name their exchange.
func TestKindsAndVenues(t *testing.T) {
	for id, want := range map[string][2]string{
		"binance:all":               {KindTicker, "binance"},
		"coinbase:BTC-USD":          {KindTicker, "coinbase"},
		"ecb":                       {KindECB, "ecb"},
		"ecb-90d":                   {KindECB, "ecb-90d"},
		"ecb-hist":                  {KindECB, "ecb-hist"},
		"coinbase-ranks":            {KindRanks, "coinbase-ranks"},
		"hist:kraken:BTC-USD":       {KindDaily, "kraken"},
		"bars:1h:okx:ETH-USDT":      {KindHistory, "okx"},
		"bars:1m:coinbase:USDT-USD": {KindHistory, "coinbase"},
	} {
		if k := KindOf(id); k != want[0] {
			t.Errorf("KindOf(%s) = %s, want %s", id, k, want[0])
		}
		if v := VenueOf(id); v != want[1] {
			t.Errorf("VenueOf(%s) = %s, want %s", id, v, want[1])
		}
	}
	if r := (Stat{}).Rate(); r != 1 {
		t.Fatalf("no calls: %v", r)
	}
	if r := (Stat{Calls: 4, OK: 3}).Rate(); r != 0.75 {
		t.Fatalf("3 of 4: %v", r)
	}
}
