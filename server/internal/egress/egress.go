// Package egress is the one way a daemon reaches the internet, and the one rule for when. The box
// used to open no connection at all; now ghost.tallyd (the market tickers, the daily candles, the
// ECB tables) and ghost.synthd (the news feeds) fetch for themselves, but only when the phone
// cannot do it for them: the phone says every quarter hour what network it is on (POST
// /v1/phone/net), and while it was on Wi-Fi within the last twenty minutes the phone is the proxy
// and the box stays off the wire. On mobile data, or with the phone out of reach, the box fetches
// what is due from where it left off. Publishers and exchanges then see the box's address, and
// only they: nothing personal travels, the requests are the public addresses in the sources list.
package egress

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// UA is the browser-like agent both the phone and the box send: a feed served to a browser is
// served to the box, and nothing in the string names LocalGhost.
const UA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

const (
	maxBody    = 3 << 20 // the cap the phone applies too
	proxyGrace = 20 * time.Minute
)

// Fetched is one address's answer, the shape the phone posts and the daemons ingest.
type Fetched struct {
	ID     string `json:"id"`
	Status int    `json:"status"` // 0 when the fetch itself failed
	Error  string `json:"error,omitempty"`
	Body   string `json:"body,omitempty"`
}

// Client fetches with a fixed agent, no cookies, short deadlines and a body cap.
type Client struct {
	hc *http.Client
}

// New is a client with sensible limits. One per daemon.
func New() *Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		DisableCompression:    false,
	}
	return &Client{hc: &http.Client{Transport: tr, Timeout: 40 * time.Second}}
}

// Get fetches one address. Every outcome is a Fetched (the ingest wants to know a failure too);
// err is set only when the context ended.
func (c *Client) Get(ctx context.Context, id, url string) (Fetched, error) {
	return c.GetCapped(ctx, id, url, maxBody)
}

// GetCapped is Get with its own body cap (the ECB's full history is seven megabytes, once).
func (c *Client) GetCapped(ctx context.Context, id, url string, cap int64) (Fetched, error) {
	out := Fetched{ID: id}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		out.Error = "bad address: " + err.Error()
		return out, nil
	}
	req.Header.Set("User-Agent", UA)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, application/json, text/xml;q=0.9, */*;q=0.5")
	resp, err := c.hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		out.Error = clip(err.Error(), 160)
		return out, nil
	}
	defer resp.Body.Close()
	out.Status = resp.StatusCode
	b, err := io.ReadAll(io.LimitReader(resp.Body, cap))
	if err != nil && !errors.Is(err, io.EOF) {
		out.Error = "read: " + clip(err.Error(), 120)
		return out, nil
	}
	out.Body = string(b)
	return out, nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// PhoneNet is what the phone last said: wifi, mobile, none, or "" when it never said.
type PhoneNet struct {
	Net  string
	Seen time.Time
}

// Proxy says whether the phone is fetching for the box right now: it said Wi-Fi within the grace.
// Pure, so the tests cover the three cases.
func (p PhoneNet) Proxy(now time.Time) (bool, string) {
	switch {
	case p.Seen.IsZero():
		return false, "the phone has not said what network it is on"
	case now.Sub(p.Seen) > proxyGrace:
		return false, "the phone was last heard " + now.Sub(p.Seen).Truncate(time.Minute).String() + " ago"
	case p.Net == "wifi":
		return true, "the phone is on Wi-Fi and fetches for the box"
	default:
		return false, "the phone is on " + p.Net
	}
}

// Due says whether something fetched last at lastUnix wants fetching again, every so often, with a
// minute's slack so an hourly job does not slip to the next tick.
func Due(lastUnix int64, every time.Duration, now time.Time) bool {
	if lastUnix <= 0 {
		return true
	}
	return now.Sub(time.Unix(lastUnix, 0)) >= every-time.Minute
}

// ParseNet keeps the phone's word to the three the box knows.
func ParseNet(s string) (string, bool) {
	switch s {
	case "wifi", "mobile", "none":
		return s, true
	}
	return "", false
}

// SeenString writes a unix time for the settings row.
func SeenString(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }
