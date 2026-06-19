// ghostbench-mm.go — multimodal daemon simulator for a llama.cpp server.
//
// Each simulated daemon:
//   1. reads an image + audio file and base64-encodes them,
//   2. extracts the image's EXIF metadata locally (pure Go, no deps),
//   3. sends image + audio + text context to the multimodal endpoint,
//      asking for a single JSON object back,
//   4. merges the model's transcription/description with the local metadata.
// N daemons fire simultaneously; it reports media token cost (prompt_n),
// prefill vs generation throughput, and aggregate.
//
// Build:  go build -o ghostbench-mm ghostbench-mm.go
// Run:    ./ghostbench-mm -n 15 -image testimage.jpg -audio testaudio.wav -pad 5000 -max 768
//
// Needs the server started with --mmproj (multimodal) and --parallel >= n,
// with -c large enough that (-c / n) covers text + media tokens + generation.

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------- request / response shapes ----------

type part struct {
	Type       string      `json:"type"`
	Text       string      `json:"text,omitempty"`
	ImageURL   *imageURL   `json:"image_url,omitempty"`
	InputAudio *inputAudio `json:"input_audio,omitempty"`
}
type imageURL struct {
	URL string `json:"url"`
}
type inputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}
type message struct {
	Role    string `json:"role"`
	Content []part `json:"content"`
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
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Timings struct {
		PromptN            int     `json:"prompt_n"`
		PromptPerSecond    float64 `json:"prompt_per_second"`
		PredictedN         int     `json:"predicted_n"`
		PredictedPerSecond float64 `json:"predicted_per_second"`
	} `json:"timings"`
}

// what we ask the model to return
type modelOut struct {
	Transcription    string   `json:"transcription"`
	ImageDescription string   `json:"image_description"`
	Objects          []string `json:"objects"`
	Scene            string   `json:"scene"`
}

type result struct {
	id         int
	promptTok  int
	genTok     int
	prefillTPS float64
	genTPS     float64
	start, end time.Time
	parsed     modelOut
	rawSample  string
	err        error
}

func main() {
	var (
		n       = flag.Int("n", 15, "concurrent daemons")
		urlBase = flag.String("url", "http://127.0.0.1:51017", "server base URL")
		imgPath = flag.String("image", "testimage.jpg", "image file")
		audPath = flag.String("audio", "testaudio.wav", "audio file (wav)")
		pad     = flag.Int("pad", 5000, "approx text-context tokens of filler")
		maxTok  = flag.Int("max", 768, "max generation tokens")
		show    = flag.Bool("show", false, "print one full merged daemon output")
	)
	flag.Parse()

	// load + encode media once; daemons share the bytes
	imgB, err := os.ReadFile(*imgPath)
	must(err)
	audB, err := os.ReadFile(*audPath)
	must(err)
	imgDataURL := "data:" + mimeForImage(*imgPath) + ";base64," + base64.StdEncoding.EncodeToString(imgB)
	audB64 := base64.StdEncoding.EncodeToString(audB)

	// local metadata extraction (the "extract everything from it" half)
	meta := readExif(imgB)
	fmt.Printf("local EXIF extracted from %s:\n", filepath.Base(*imgPath))
	for k, v := range meta {
		fmt.Printf("  %-16s %s\n", k, v)
	}
	fmt.Println()

	instruction := "You are given an image and an audio clip. Respond with ONE JSON object and nothing else " +
		"(no markdown, no code fences). Keys: transcription (string, what the audio says or contains), " +
		"image_description (string), objects (array of strings visible in the image), scene (string). " + filler(*pad)

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
				MaxTokens:   *maxTok,
				Temperature: 0,
				Stream:      false,
				CachePrompt: false,
				Messages: []message{{
					Role: "user",
					Content: []part{
						{Type: "text", Text: instruction},
						{Type: "image_url", ImageURL: &imageURL{URL: imgDataURL}},
						{Type: "input_audio", InputAudio: &inputAudio{Data: audB64, Format: "wav"}},
					},
				}},
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
				results[id] = result{id: id, err: fmt.Errorf("status %d: %s", resp.StatusCode, trunc(raw, 200))}
				return
			}
			var cr chatResponse
			if err := json.Unmarshal(raw, &cr); err != nil {
				results[id] = result{id: id, err: err}
				return
			}
			content := ""
			if len(cr.Choices) > 0 {
				content = cr.Choices[0].Message.Content
			}
			var mo modelOut
			_ = json.Unmarshal([]byte(extractJSON(content)), &mo) // tolerate prose around JSON

			gen := pick(cr.Timings.PredictedN, cr.Usage.CompletionTokens)
			pr := pick(cr.Timings.PromptN, cr.Usage.PromptTokens)
			results[id] = result{
				id: id, promptTok: pr, genTok: gen,
				prefillTPS: cr.Timings.PromptPerSecond, genTPS: cr.Timings.PredictedPerSecond,
				start: start, end: end, parsed: mo, rawSample: content,
			}
		}(i)
	}

	fmt.Printf("firing %d multimodal daemons at %s (image+audio+~%d text tokens, no cache)\n\n", *n, *urlBase, *pad)
	wallStart := time.Now()
	close(gun)
	wg.Wait()
	wallEnd := time.Now()

	var totalPrompt, totalGen, ok int
	firstStart, lastEnd := wallEnd, wallStart
	var genTPSes, prefillTPSes []float64
	var sample *result
	for i := range results {
		r := results[i]
		if r.err != nil {
			fmt.Printf("daemon %2d  ERROR: %v\n", r.id, r.err)
			continue
		}
		ok++
		totalPrompt += r.promptTok
		totalGen += r.genTok
		genTPSes = append(genTPSes, r.genTPS)
		prefillTPSes = append(prefillTPSes, r.prefillTPS)
		if r.start.Before(firstStart) {
			firstStart = r.start
		}
		if r.end.After(lastEnd) {
			lastEnd = r.end
		}
		if sample == nil {
			sample = &results[i]
		}
		fmt.Printf("daemon %2d  prompt %5d  gen %4d  %6.2fs  prefill %7.1f t/s  gen %6.2f t/s\n",
			r.id, r.promptTok, r.genTok, r.end.Sub(r.start).Seconds(), r.prefillTPS, r.genTPS)
	}
	if ok == 0 {
		fmt.Println("\nall daemons failed")
		return
	}
	window := lastEnd.Sub(firstStart).Seconds()
	sort.Float64s(genTPSes)
	sort.Float64s(prefillTPSes)
	fmt.Printf("\n--- aggregate (%d/%d ok) ---\n", ok, *n)
	fmt.Printf("prompt tokens total : %d   (median %d/req — this is your media+text cost)\n", totalPrompt, totalPrompt/ok)
	fmt.Printf("gen tokens total    : %d\n", totalGen)
	fmt.Printf("wall window         : %.2fs\n", window)
	fmt.Printf("aggregate gen t/s   : %.2f\n", float64(totalGen)/window)
	fmt.Printf("aggregate total t/s : %.2f   ((prompt+gen)/window — true work rate)\n", float64(totalPrompt+totalGen)/window)
	fmt.Printf("median per-req gen  : %.2f t/s\n", genTPSes[len(genTPSes)/2])
	fmt.Printf("median per-req prefill: %.1f t/s   (image+audio re-encoded every request, no cache)\n", prefillTPSes[len(prefillTPSes)/2])

	if *show && sample != nil {
		merged := map[string]any{
			"transcription":     sample.parsed.Transcription,
			"image_description": sample.parsed.ImageDescription,
			"objects":           sample.parsed.Objects,
			"scene":             sample.parsed.Scene,
			"exif_metadata":     meta,
		}
		out, _ := json.MarshalIndent(merged, "", "  ")
		fmt.Printf("\n--- sample merged daemon output (model JSON + local EXIF) ---\n%s\n", out)
	}
}

// ---------- minimal pure-Go EXIF reader (IFD0 + Exif sub-IFD, ASCII tags) ----------

func readExif(jpeg []byte) map[string]string {
	out := map[string]string{}
	// find APP1 "Exif\0\0"
	i := 2 // skip SOI 0xFFD8
	var tiff []byte
	for i+4 < len(jpeg) {
		if jpeg[i] != 0xFF {
			break
		}
		marker := jpeg[i+1]
		size := int(binary.BigEndian.Uint16(jpeg[i+2 : i+4]))
		if marker == 0xE1 && i+4+6 <= len(jpeg) && string(jpeg[i+4:i+10]) == "Exif\x00\x00" {
			tiff = jpeg[i+10 : i+2+size]
			break
		}
		if marker == 0xDA { // start of scan, no more metadata
			break
		}
		i += 2 + size
	}
	if tiff == nil {
		return out
	}
	var bo binary.ByteOrder
	switch string(tiff[0:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return out
	}
	names := map[uint16]string{
		0x010e: "ImageDescription", 0x010f: "Make", 0x0110: "Model",
		0x0131: "Software", 0x0132: "DateTime", 0x9286: "UserComment",
	}
	readIFD := func(off uint32) uint32 {
		if int(off)+2 > len(tiff) {
			return 0
		}
		count := bo.Uint16(tiff[off : off+2])
		var exifPtr uint32
		p := off + 2
		for e := 0; e < int(count); e++ {
			if int(p)+12 > len(tiff) {
				break
			}
			tag := bo.Uint16(tiff[p : p+2])
			typ := bo.Uint16(tiff[p+2 : p+4])
			n := bo.Uint32(tiff[p+4 : p+8])
			valOff := tiff[p+8 : p+12]
			if tag == 0x8769 { // Exif sub-IFD pointer
				exifPtr = bo.Uint32(valOff)
			} else if name, okk := names[tag]; okk && (typ == 2 || typ == 7) {
				var s []byte
				if n <= 4 {
					s = valOff[:n]
				} else {
					o := bo.Uint32(valOff)
					if int(o)+int(n) <= len(tiff) {
						s = tiff[o : o+n]
					}
				}
				out[name] = strings.TrimRight(strings.TrimPrefix(string(s), "ASCII\x00\x00\x00"), "\x00 ")
			}
			p += 12
		}
		return exifPtr
	}
	ifd0 := bo.Uint32(tiff[4:8])
	if ep := readIFD(ifd0); ep != 0 {
		readIFD(ep)
	}
	return out
}

// ---------- helpers ----------

func extractJSON(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "```json"))
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	a, b := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if a >= 0 && b > a {
		return s[a : b+1]
	}
	return s
}
func filler(approx int) string {
	if approx <= 0 {
		return ""
	}
	words := approx * 4 / 3
	base := strings.Fields("the daemon ingests the payload and reconciles it against prior state before " +
		"writing through the log under sustained concurrent load across the node fleet")
	var b strings.Builder
	b.WriteString(" Additional operational context follows: ")
	for i := 0; i < words; i++ {
		b.WriteString(base[i%len(base)])
		b.WriteByte(' ')
	}
	return b.String()
}
func mimeForImage(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
func pick(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
func trunc(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}
func must(err error) {
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}
}