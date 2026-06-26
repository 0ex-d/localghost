// cmd/framed/main.go — ghost.framed: walk a folder of images, extract a journal
// entry from each via the local multimodal model, print and write entries as it goes.
//
// The extraction instruction is a FIXED constant sent before every image, so
// llama.cpp's prefix cache reuses it across the run (cache_prompt:true). The
// image is the only thing that changes per request. Originals are never moved
// or copied — each entry references the image path on disk, per the
// "the original does not move" principle.
//
// Build:  go build -o bin/framed ./cmd/framed
// Run:    ./bin/framed                       # defaults to ./test
//         ./bin/framed -dir ~/photos -workers 4
//
// Server: start llama-server with --mmproj and --parallel >= workers.
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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- FIXED cacheable prefix: identical on every call, nothing variable in here ----
const journalInstruction = `You are ghost.framed, a local memory daemon. You are given a single photograph or screenshot. ` +
	`Extract a journal entry from it: a short, factual, neutral record of what the image documents, suitable for a personal memory layer. ` +
	`Read any legible text in the image verbatim. Identify people by visible description only (never invent names), places, and notable objects. Suggest concise tags. ` +
	`Make a tentative significance guess (ordinary, notable, or anchor-candidate); this is only a suggestion — the user makes the final call. ` +
	`Respond with exactly one JSON object and nothing else: no markdown, no code fences. Keys: ` +
	`entry (string), summary (string, one sentence), text_in_image (string, verbatim or empty), ` +
	`people (array of strings), places (array of strings), tags (array of strings), ` +
	`significance_guess (one of: ordinary, notable, anchor-candidate). Do not invent details the image does not support.`

type part struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}
type imageURL struct {
	URL string `json:"url"`
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
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
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

type extracted struct {
	Entry        string   `json:"entry"`
	Summary      string   `json:"summary"`
	TextInImage  string   `json:"text_in_image"`
	People       []string `json:"people"`
	Places       []string `json:"places"`
	Tags         []string `json:"tags"`
	Significance string   `json:"significance_guess"`
}

type journalEntry struct {
	Source       string            `json:"source"`      // image path — original, preserved in place
	CapturedAt   string            `json:"captured_at"` // EXIF capture time if present
	Coordinates  string            `json:"coordinates"` // "lat, lon" from GPS EXIF, if present
	Latitude     string            `json:"latitude,omitempty"`
	Longitude    string            `json:"longitude,omitempty"`
	Altitude     string            `json:"altitude_m,omitempty"`
	GPSTimeUTC   string            `json:"gps_time_utc,omitempty"`
	ProcessedAt  string            `json:"processed_at"`
	Entry        string            `json:"entry"`
	Summary      string            `json:"summary"`
	TextInImage  string            `json:"text_in_image"`
	People       []string          `json:"people"`
	Places       []string          `json:"places"`
	Tags         []string          `json:"tags"`
	Significance string            `json:"significance_guess"` // model guesses; user decides
	Exif         map[string]string `json:"exif"`
	PromptTokens int               `json:"prompt_tokens"`
	GenTokens    int               `json:"gen_tokens"`
	PrefillTPS   float64           `json:"prefill_tps"`
	ImageBytes   int               `json:"image_bytes"`
	Raw          string            `json:"raw,omitempty"` // model's verbatim reply, kept on failure
	Error        string            `json:"error,omitempty"`
}

func main() {
	var (
		dir     = flag.String("dir", "test", "folder of images to process")
		outDir  = flag.String("out", "journal", "output folder for journal entries")
		urlBase = flag.String("url", "http://127.0.0.1:51017", "server base URL")
		workers = flag.Int("workers", 4, "concurrent workers (keep <= server --parallel; use 1 for ordered output)")
		maxTok  = flag.Int("max", 512, "max generation tokens per image")
		model   = flag.String("model", "local", "model alias")
	)
	flag.Parse()

	images := findImages(*dir)
	if len(images) == 0 {
		fmt.Printf("no images found in %s\n", *dir)
		os.Exit(1)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Println("cannot create out dir:", err)
		os.Exit(1)
	}
	jsonl, err := os.Create(filepath.Join(*outDir, "journal.jsonl"))
	if err != nil {
		fmt.Println("cannot create jsonl:", err)
		os.Exit(1)
	}
	defer jsonl.Close()

	fmt.Printf("ghost.framed: %d images in %s, %d worker(s), instruction cached across run\n", len(images), *dir, *workers)

	client := &http.Client{Timeout: 300 * time.Second}
	jobs := make(chan string)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var done, failed int

	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				je := process(client, *urlBase, *model, path, *maxTok)

				outName := filepath.Join(*outDir, sanitise(filepath.Base(path))+".json")
				if b, e := json.MarshalIndent(je, "", "  "); e == nil {
					_ = os.WriteFile(outName, b, 0o644)
				}

				mu.Lock()
				done++
				if je.Error != "" {
					failed++
				}
				if line, e := json.Marshal(je); e == nil {
					jsonl.Write(line)
					jsonl.Write([]byte("\n"))
				}
				printEntry(done, len(images), path, je) // full entry, atomic under lock
				mu.Unlock()
			}
		}()
	}
	start := time.Now()
	for _, p := range images {
		jobs <- p
	}
	close(jobs)
	wg.Wait()

	fmt.Printf("\n%s\n", strings.Repeat("=", 72))
	fmt.Printf("done: %d entries (%d failed) in %.1fs → %s/\n", len(images)-failed, failed, time.Since(start).Seconds(), *outDir)
}

// printEntry renders one full journal entry as a self-contained block.
func printEntry(idx, total int, path string, je journalEntry) {
	fmt.Printf("\n%s\n", strings.Repeat("─", 72))
	cap := je.CapturedAt
	if cap == "" {
		cap = "no capture date"
	}
	fmt.Printf("[%d/%d] %s   %s   %dKB  prompt %d  prefill %.0f t/s\n",
		idx, total, filepath.Base(path), cap, je.ImageBytes/1024, je.PromptTokens, je.PrefillTPS)
	if je.Error != "" {
		fmt.Printf("  ERROR: %s\n", je.Error)
		if je.PromptTokens > 0 && je.PromptTokens < 800 {
			fmt.Printf("  NOTE: prompt is only %d tokens — the image did NOT reach the model (instruction alone is ~472)\n", je.PromptTokens)
		}
		if je.Raw != "" {
			fmt.Printf("  model said: %s\n", truncate(je.Raw, 400))
		}
		return
	}
	if je.Summary != "" {
		fmt.Printf("  summary: %s\n", je.Summary)
	}
	if je.Entry != "" {
		fmt.Printf("  entry:\n%s\n", indent(wrap(je.Entry, 68), "    "))
	}
	if strings.TrimSpace(je.TextInImage) != "" {
		fmt.Printf("  text in image: %s\n", truncate(je.TextInImage, 200))
	}
	if len(je.People) > 0 {
		fmt.Printf("  people: %s\n", strings.Join(je.People, ", "))
	}
	if len(je.Places) > 0 {
		fmt.Printf("  places: %s\n", strings.Join(je.Places, ", "))
	}
	if len(je.Tags) > 0 {
		fmt.Printf("  tags:   %s\n", strings.Join(je.Tags, ", "))
	}
	if je.Significance != "" {
		fmt.Printf("  significance (guess, you decide): %s\n", je.Significance)
	}
	// location & time pulled from the image's own metadata
	if je.Coordinates != "" {
		line := "  location: " + je.Coordinates
		if je.Altitude != "" {
			line += "  alt " + je.Altitude + "m"
		}
		line += "  https://maps.google.com/?q=" + je.Coordinates
		fmt.Println(strings.ReplaceAll(line, " https", "  https"))
	}
	when := je.CapturedAt
	if je.GPSTimeUTC != "" {
		when += "  (GPS UTC " + je.GPSTimeUTC + ")"
	}
	if strings.TrimSpace(when) != "" {
		fmt.Printf("  taken: %s\n", when)
	}
}

func process(client *http.Client, urlBase, model, path string, maxTok int) journalEntry {
	je := journalEntry{Source: path, ProcessedAt: time.Now().UTC().Format(time.RFC3339)}

	raw, err := os.ReadFile(path)
	if err != nil {
		je.Error = "read: " + err.Error()
		return je
	}
	if len(raw) == 0 {
		je.Error = "empty image file"
		return je
	}
	je.ImageBytes = len(raw)
	je.Exif = readExif(raw)
	if dt := je.Exif["DateTime"]; dt != "" {
		je.CapturedAt = dt
	}
	je.Coordinates = je.Exif["coordinates"]
	je.Latitude = je.Exif["lat"]
	je.Longitude = je.Exif["lon"]
	je.Altitude = je.Exif["altitude_m"]
	je.GPSTimeUTC = je.Exif["gps_time_utc"]
	dataURL := "data:" + mimeForImage(path) + ";base64," + base64.StdEncoding.EncodeToString(raw)

	body, _ := json.Marshal(chatRequest{
		Model: model, MaxTokens: maxTok, Temperature: 0, Stream: false, CachePrompt: true,
		Messages: []message{{Role: "user", Content: []part{
			{Type: "text", Text: journalInstruction}, // FIXED prefix — caches
			{Type: "image_url", ImageURL: &imageURL{URL: dataURL}},
		}}},
	})

	resp, err := client.Post(urlBase+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		je.Error = "post: " + err.Error()
		return je
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		je.Error = fmt.Sprintf("status %d: %s", resp.StatusCode, truncate(string(respBody), 120))
		return je
	}
	var cr chatResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		je.Error = "decode: " + err.Error()
		return je
	}
	content := ""
	if len(cr.Choices) > 0 {
		content = cr.Choices[0].Message.Content
		if strings.TrimSpace(content) == "" {
			// this Gemma 4 build returns the answer in a separate reasoning channel
			content = cr.Choices[0].Message.ReasoningContent
		}
	}
	var ex extracted
	if err := json.Unmarshal([]byte(extractJSON(content)), &ex); err != nil {
		je.Error = "model did not return valid JSON"
		je.Raw = content // keep verbatim so we can see what it actually said
	} else {
		je.Entry, je.Summary, je.TextInImage = ex.Entry, ex.Summary, ex.TextInImage
		je.People, je.Places, je.Tags, je.Significance = ex.People, ex.Places, ex.Tags, ex.Significance
	}
	je.PromptTokens = pick(cr.Timings.PromptN, cr.Usage.PromptTokens)
	je.GenTokens = pick(cr.Timings.PredictedN, cr.Usage.CompletionTokens)
	je.PrefillTPS = cr.Timings.PromptPerSecond
	return je
}

func findImages(dir string) []string {
	var out []string
	exts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if exts[strings.ToLower(filepath.Ext(p))] {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// ---- pure-Go EXIF reader: IFD0, Exif sub-IFD (ASCII tags), and GPS sub-IFD ----
// Returns string tags plus, when present, decimal "lat"/"lon"/"coordinates",
// "altitude_m" and "gps_time_utc". No external dependencies.
func readExif(jpeg []byte) map[string]string {
	out := map[string]string{}
	if len(jpeg) < 4 || jpeg[0] != 0xFF || jpeg[1] != 0xD8 {
		return out
	}
	// Walk ALL APP1 segments — a JPEG carries several, and EXIF and XMP live in
	// separate markers. XMP can also be split into a main packet plus one or more
	// "extended" packets. We collect both and parse whichever has location.
	i := 2
	var tiff, xmp []byte
	for i+4 <= len(jpeg) {
		if jpeg[i] != 0xFF {
			break
		}
		marker := jpeg[i+1]
		if marker == 0xD9 || marker == 0xDA { // EOI / start of scan: pixel data begins
			break
		}
		if i+4 > len(jpeg) {
			break
		}
		size := int(binary.BigEndian.Uint16(jpeg[i+2 : i+4]))
		if size < 2 || i+2+size > len(jpeg) {
			break
		}
		seg := jpeg[i+4 : i+2+size]
		if marker == 0xE1 {
			switch {
			case len(seg) >= 6 && string(seg[0:6]) == "Exif\x00\x00":
				if tiff == nil {
					tiff = seg[6:]
				}
			case len(seg) >= 29 && string(seg[0:29]) == "http://ns.adobe.com/xap/1.0/\x00":
				xmp = append(xmp, seg[29:]...)
			case len(seg) >= 35 && string(seg[0:35]) == "http://ns.adobe.com/xmp/extension/\x00":
				// extended XMP header: 35b namespace + 32b GUID + 4b full-len + 4b offset
				if len(seg) > 75 {
					xmp = append(xmp, seg[75:]...)
				}
			}
		}
		i += 2 + size
	}

	// XMP-only files (EXIF absent or unparseable) still get a GPS pass at the end.
	if tiff == nil || len(tiff) < 8 {
		if len(xmp) > 0 {
			parseXMPGPS(string(xmp), out)
		}
		return out
	}
	var bo binary.ByteOrder
	switch string(tiff[0:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		if len(xmp) > 0 {
			parseXMPGPS(string(xmp), out)
		}
		return out
	}

	rat := func(off uint32) float64 { // URATIONAL: num/den
		if int(off)+8 > len(tiff) {
			return 0
		}
		num := bo.Uint32(tiff[off : off+4])
		den := bo.Uint32(tiff[off+4 : off+8])
		if den == 0 {
			return 0
		}
		return float64(num) / float64(den)
	}
	dms := func(off uint32) float64 { // 3 rationals deg/min/sec -> decimal
		return rat(off) + rat(off+8)/60 + rat(off+16)/3600
	}
	ascii := func(typ uint16, n uint32, valOff []byte) string {
		if typ != 2 && typ != 7 {
			return ""
		}
		var s []byte
		if n <= 4 {
			s = valOff[:n]
		} else {
			o := bo.Uint32(valOff)
			if int(o)+int(n) <= len(tiff) {
				s = tiff[o : o+n]
			}
		}
		return strings.TrimRight(strings.TrimPrefix(string(s), "ASCII\x00\x00\x00"), "\x00 ")
	}

	names := map[uint16]string{
		0x010e: "ImageDescription", 0x010f: "Make", 0x0110: "Model",
		0x0131: "Software", 0x0132: "DateTime", 0x9286: "UserComment",
		0x9003: "DateTimeOriginal",
	}

	readGPS := func(off uint32) {
		if int(off)+2 > len(tiff) {
			return
		}
		count := bo.Uint16(tiff[off : off+2])
		p := off + 2
		var latRef, lonRef, altRef, gpsDate, gpsTime string
		var lat, lon, alt float64
		var haveLat, haveLon, haveAlt bool
		for e := 0; e < int(count); e++ {
			if int(p)+12 > len(tiff) {
				break
			}
			tag := bo.Uint16(tiff[p : p+2])
			typ := bo.Uint16(tiff[p+2 : p+4])
			n := bo.Uint32(tiff[p+4 : p+8])
			valOff := tiff[p+8 : p+12]
			switch tag {
			case 0x0001:
				latRef = ascii(typ, n, valOff)
			case 0x0002:
				lat = dms(bo.Uint32(valOff))
				haveLat = true
			case 0x0003:
				lonRef = ascii(typ, n, valOff)
			case 0x0004:
				lon = dms(bo.Uint32(valOff))
				haveLon = true
			case 0x0005:
				if n <= 4 && len(valOff) > 0 && valOff[0] == 1 {
					altRef = "-"
				}
			case 0x0006:
				alt = rat(bo.Uint32(valOff))
				haveAlt = true
			case 0x001d:
				gpsDate = ascii(typ, n, valOff)
			case 0x0007:
				o := bo.Uint32(valOff)
				gpsTime = fmt.Sprintf("%02.0f:%02.0f:%02.0f", rat(o), rat(o+8), rat(o+16))
			}
			p += 12
		}
		if haveLat && haveLon && (lat != 0 || lon != 0) {
			if latRef == "S" {
				lat = -lat
			}
			if lonRef == "W" {
				lon = -lon
			}
			out["lat"] = fmt.Sprintf("%.6f", lat)
			out["lon"] = fmt.Sprintf("%.6f", lon)
			out["coordinates"] = fmt.Sprintf("%.6f, %.6f", lat, lon)
		}
		if haveAlt && alt != 0 {
			out["altitude_m"] = fmt.Sprintf("%s%.1f", altRef, alt)
		}
		if gpsDate != "" || gpsTime != "" {
			out["gps_time_utc"] = strings.TrimSpace(gpsDate + " " + gpsTime)
		}
	}

	readIFD := func(off uint32) (exifPtr, gpsPtr uint32) {
		if int(off)+2 > len(tiff) {
			return
		}
		count := bo.Uint16(tiff[off : off+2])
		p := off + 2
		for e := 0; e < int(count); e++ {
			if int(p)+12 > len(tiff) {
				break
			}
			tag := bo.Uint16(tiff[p : p+2])
			typ := bo.Uint16(tiff[p+2 : p+4])
			n := bo.Uint32(tiff[p+4 : p+8])
			valOff := tiff[p+8 : p+12]
			switch tag {
			case 0x8769:
				exifPtr = bo.Uint32(valOff)
			case 0x8825:
				gpsPtr = bo.Uint32(valOff)
			default:
				if name, okk := names[tag]; okk {
					if v := ascii(typ, n, valOff); v != "" {
						out[name] = v
					}
				}
			}
			p += 12
		}
		return
	}

	exifPtr, gpsPtr := readIFD(bo.Uint32(tiff[4:8]))
	if exifPtr != 0 {
		_, gp := readIFD(exifPtr) // DateTimeOriginal lives in the Exif IFD
		if gpsPtr == 0 {
			gpsPtr = gp
		}
	}
	if gpsPtr != 0 {
		readGPS(gpsPtr)
	}
	// Fallback: if EXIF carried no usable coordinates (absent, or zeroed by an
	// upload/redaction step), try XMP, where Samsung/Google often keep them.
	if _, ok := out["coordinates"]; !ok && len(xmp) > 0 {
		parseXMPGPS(string(xmp), out)
	}
	if dto := out["DateTimeOriginal"]; dto != "" {
		out["DateTime"] = dto
	}
	return out
}

// parseXMPGPS pulls GPS out of an XMP packet (RDF/XML text). Handles both the
// attribute form  exif:GPSLatitude="51,29.58N"  and the element form
// <exif:GPSLatitude>51,29.58N</exif:GPSLatitude>, in any namespace prefix.
func parseXMPGPS(xmp string, out map[string]string) {
	get := func(tag string) string {
		// attribute:  ...GPSLatitude="VALUE"
		if re := regexp.MustCompile(`(?:[A-Za-z]+:)?` + tag + `\s*=\s*"([^"]*)"`); true {
			if m := re.FindStringSubmatch(xmp); m != nil && strings.TrimSpace(m[1]) != "" {
				return strings.TrimSpace(m[1])
			}
		}
		// element:  <ns:GPSLatitude>VALUE</ns:GPSLatitude>
		re := regexp.MustCompile(`<(?:[A-Za-z]+:)?` + tag + `>([^<]*)</`)
		if m := re.FindStringSubmatch(xmp); m != nil {
			return strings.TrimSpace(m[1])
		}
		return ""
	}
	lat, okLat := xmpCoord(get("GPSLatitude"))
	lon, okLon := xmpCoord(get("GPSLongitude"))
	if okLat && okLon && (lat != 0 || lon != 0) {
		out["lat"] = fmt.Sprintf("%.6f", lat)
		out["lon"] = fmt.Sprintf("%.6f", lon)
		out["coordinates"] = fmt.Sprintf("%.6f, %.6f", lat, lon)
		out["gps_source"] = "xmp"
	}
	if alt := get("GPSAltitude"); alt != "" {
		// XMP altitude is often a rational "123/1"
		if p := strings.SplitN(alt, "/", 2); len(p) == 2 {
			a, _ := strconv.ParseFloat(p[0], 64)
			b, _ := strconv.ParseFloat(p[1], 64)
			if b != 0 && a != 0 {
				out["altitude_m"] = fmt.Sprintf("%.1f", a/b)
			}
		} else if a, err := strconv.ParseFloat(alt, 64); err == nil && a != 0 {
			out["altitude_m"] = fmt.Sprintf("%.1f", a)
		}
	}
}

// xmpCoord converts an XMP GPS coordinate to signed decimal degrees.
// Accepts "DDD,MM.mmmmH" (deg, decimal-minutes, hemisphere), "DDD,MM,SS.sH",
// or plain decimal "DDD.dddd" optionally suffixed with a hemisphere letter.
func xmpCoord(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	var hemi byte
	if last := v[len(v)-1]; last == 'N' || last == 'S' || last == 'E' || last == 'W' {
		hemi = last
		v = strings.TrimSpace(v[:len(v)-1])
	}
	parts := strings.Split(v, ",")
	var deg float64
	switch len(parts) {
	case 1:
		f, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil {
			return 0, false
		}
		deg = f
	case 2:
		d, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		m, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if e1 != nil || e2 != nil {
			return 0, false
		}
		deg = d + m/60
	case 3:
		d, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		m, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		s, e3 := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		if e1 != nil || e2 != nil || e3 != nil {
			return 0, false
		}
		deg = d + m/60 + s/3600
	default:
		return 0, false
	}
	if hemi == 'S' || hemi == 'W' {
		deg = -deg
	}
	return deg, true
}

// ---- helpers ----
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	a, b := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if a >= 0 && b > a {
		return s[a : b+1]
	}
	return s
}
func wrap(s string, width int) string {
	var b strings.Builder
	for li, line := range strings.Split(s, "\n") {
		if li > 0 {
			b.WriteByte('\n')
		}
		col := 0
		for wi, word := range strings.Fields(line) {
			if wi > 0 {
				if col+1+len(word) > width {
					b.WriteByte('\n')
					col = 0
				} else {
					b.WriteByte(' ')
					col++
				}
			}
			b.WriteString(word)
			col += len(word)
		}
	}
	return b.String()
}
func indent(s, pad string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}
func mimeForImage(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/jpeg"
	}
}
func sanitise(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
}
func pick(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}