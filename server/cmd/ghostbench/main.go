// ghostbench.go — concurrent throughput benchmark for a llama.cpp server.
//
// Fires N chat completions simultaneously, each carrying a realistic context
// payload, and reports prefill vs generation throughput. Stdlib only.
//
// Build:  go build -o ghostbench ghostbench.go
// Run:    ./ghostbench -n 15 -pad 5000 -max 1024 -url http://127.0.0.1:51017
//   or:   ./ghostbench -n 15 -promptfile real_context.txt -max 1024
//
// Hit the local port directly, not nginx, to measure the model rather than
// TLS + proxy. Requires the server started with --parallel >= n and a total
// -c large enough that (-c / n) covers your prompt + generation, or requests
// truncate. cache_prompt is sent false so every request does a full prefill,
// matching a "fresh context injected each time" workload.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
	Stream      bool      `json:"stream"`
	CachePrompt bool      `json:"cache_prompt"`
}

type chatResponse struct {
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	// llama.cpp extension: per-request server-side timings.
	Timings struct {
		PromptN            int     `json:"prompt_n"`
		PromptPerSecond    float64 `json:"prompt_per_second"`
		PredictedN         int     `json:"predicted_n"`
		PredictedPerSecond float64 `json:"predicted_per_second"`
	} `json:"timings"`
}

type result struct {
	id         int
	promptTok  int
	genTok     int
	prefillTPS float64 // server-reported prompt eval t/s
	genTPS     float64 // server-reported generation t/s
	start      time.Time
	end        time.Time
	err        error
}

func main() {
	var (
		n          = flag.Int("n", 15, "number of concurrent requests")
		urlBase    = flag.String("url", "http://127.0.0.1:51017", "server base URL")
		maxTok     = flag.Int("max", 1024, "max tokens to generate per request")
		pad        = flag.Int("pad", 5000, "approx context tokens of synthetic filler (ignored if -promptfile set)")
		promptfile = flag.String("promptfile", "", "file whose contents become the per-request context")
	)
	flag.Parse()

	context := filler(*pad)
	if *promptfile != "" {
		raw, err := os.ReadFile(*promptfile)
		if err != nil {
			fmt.Printf("cannot read promptfile: %v\n", err)
			os.Exit(1)
		}
		context = string(raw)
	}
	prompt := context + "\n\nGiven the context above, write a thorough technical analysis."

	client := &http.Client{Timeout: 600 * time.Second}
	gun := make(chan struct{})
	results := make([]result, *n)
	var wg sync.WaitGroup

	for i := 0; i < *n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			body, _ := json.Marshal(chatRequest{
				Model:       "local",
				Messages:    []message{{Role: "user", Content: prompt}},
				MaxTokens:   *maxTok,
				Temperature: 0,
				Stream:      false,
				CachePrompt: false,
			})

			<-gun
			start := time.Now()

			req, _ := http.NewRequest("POST", *urlBase+"/v1/chat/completions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				results[id] = result{id: id, err: err}
				return
			}
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			end := time.Now()

			if resp.StatusCode != http.StatusOK {
				results[id] = result{id: id, err: fmt.Errorf("status %d: %s", resp.StatusCode, truncate(raw, 200))}
				return
			}

			var cr chatResponse
			if err := json.Unmarshal(raw, &cr); err != nil {
				results[id] = result{id: id, err: err}
				return
			}

			genTok := cr.Timings.PredictedN
			if genTok == 0 {
				genTok = cr.Usage.CompletionTokens
			}
			promptTok := cr.Timings.PromptN
			if promptTok == 0 {
				promptTok = cr.Usage.PromptTokens
			}
			results[id] = result{
				id:         id,
				promptTok:  promptTok,
				genTok:     genTok,
				prefillTPS: cr.Timings.PromptPerSecond,
				genTPS:     cr.Timings.PredictedPerSecond,
				start:      start,
				end:        end,
			}
		}(i)
	}

	fmt.Printf("firing %d concurrent requests at %s\n", *n, *urlBase)
	fmt.Printf("context ~%d tokens each (no cache), generating up to %d\n\n", *pad, *maxTok)
	wallStart := time.Now()
	close(gun)
	wg.Wait()
	wallEnd := time.Now()

	var (
		totalPrompt, totalGen, ok int
		firstStart                = wallEnd
		lastEnd                   = wallStart
		genTPSes                  []float64
		prefillTPSes              []float64
	)
	for _, r := range results {
		if r.err != nil {
			fmt.Printf("req %2d  ERROR: %v\n", r.id, r.err)
			continue
		}
		ok++
		totalPrompt += r.promptTok
		totalGen += r.genTok
		dur := r.end.Sub(r.start).Seconds()
		genTPSes = append(genTPSes, r.genTPS)
		prefillTPSes = append(prefillTPSes, r.prefillTPS)
		if r.start.Before(firstStart) {
			firstStart = r.start
		}
		if r.end.After(lastEnd) {
			lastEnd = r.end
		}
		fmt.Printf("req %2d  prompt %5d  gen %4d  %6.2fs  prefill %7.1f t/s  gen %6.2f t/s\n",
			r.id, r.promptTok, r.genTok, dur, r.prefillTPS, r.genTPS)
	}

	if ok == 0 {
		fmt.Println("\nall requests failed")
		return
	}

	window := lastEnd.Sub(firstStart).Seconds()
	sort.Float64s(genTPSes)
	sort.Float64s(prefillTPSes)

	fmt.Printf("\n--- aggregate (%d/%d ok) ---\n", ok, *n)
	fmt.Printf("prompt tokens total : %d\n", totalPrompt)
	fmt.Printf("gen tokens total    : %d\n", totalGen)
	fmt.Printf("wall window         : %.2fs\n", window)
	fmt.Printf("aggregate gen t/s   : %.2f   (gen tokens / wall window, all slots)\n", float64(totalGen)/window)
	fmt.Printf("aggregate total t/s : %.2f   ((prompt+gen) / wall window, total work)\n", float64(totalPrompt+totalGen)/window)
	fmt.Printf("median per-req gen  : %.2f t/s\n", genTPSes[len(genTPSes)/2])
	fmt.Printf("median per-req prefill: %.1f t/s\n", prefillTPSes[len(prefillTPSes)/2])
}

// filler builds roughly approxTokens of English-ish text (~1.33 words/token).
// The server reports the actual prompt_n so you see the real count per request.
func filler(approxTokens int) string {
	words := approxTokens * 4 / 3
	base := strings.Fields("the system processes each record through a write ahead log before committing state to persistent storage ensuring durability and crash recovery across the distributed node fleet under sustained concurrent load")
	var b strings.Builder
	for i := 0; i < words; i++ {
		b.WriteString(base[i%len(base)])
		b.WriteByte(' ')
	}
	return b.String()
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}