package oracled

// A Backend is one concrete model oracled can route to. llamaBackend runs llama.cpp's llama-server as
// a PRIVATE child , bound to loopback on a port nobody else is told, weights loaded from the encrypted
// volume , so the model is "run directly" (no separate service, no exposure) without dragging GGML
// into this Go binary via cgo. A frontierBackend (an HTTPS call to a remote API) implements the same
// interface, so swapping local gemma for a frontier model is a conf change, invisible to the queue and
// the callers.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/oracle"
)

// Backend serves one inference. The queue's single worker calls Infer one request at a time.
type Backend interface {
	Name() string
	Infer(ctx context.Context, req oracle.Request) (oracle.Response, error)
}

// LlamaConfig configures the llama-server child. Paths to the weights are on the ENCRYPTED VOLUME, so
// a locked box cannot read the model , consistent with everything else on the box.
type LlamaConfig struct {
	BinPath   string // llama-server binary, e.g. /usr/local/bin/llama-server (the binary is not secret)
	ModelPath string // <mount>/ai-models/gemma-4-12b-it-Q4_K_M.gguf , weights ON the volume
	MmprojPath string // <mount>/ai-models/mmproj-F16.gguf (multimodal projector), optional
	Port      int    // loopback port oracled picks and tells no one
	ModelName string // reported back in Response.Model, e.g. "gemma-4-12b"
	ExtraArgs []string
}

// llamaBackend owns a llama-server subprocess.
type llamaBackend struct {
	cfg    LlamaConfig
	proc   *os.Process
	client *http.Client
	addr   string
}

// NewLlamaBackend prepares (does not start) the backend.
func NewLlamaBackend(cfg LlamaConfig) *llamaBackend {
	return &llamaBackend{
		cfg:    cfg,
		client: &http.Client{Timeout: 120 * time.Second}, // a 12B generation can be slow
		addr:   "127.0.0.1:" + strconv.Itoa(cfg.Port),
	}
}

func (b *llamaBackend) Name() string { return b.cfg.ModelName }

// Start launches llama-server on loopback and waits for /health to go green before returning, so the
// queue never dispatches at a model still loading its weights. weights-load for a 12B Q4 model takes
// seconds to tens of seconds, so the wait is generous.
func (b *llamaBackend) Start(ctx context.Context) error {
	args := []string{
		"-m", b.cfg.ModelPath,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(b.cfg.Port),
	}
	if b.cfg.MmprojPath != "" {
		args = append(args, "--mmproj", b.cfg.MmprojPath)
	}
	args = append(args, b.cfg.ExtraArgs...)

	cmd := exec.Command(b.cfg.BinPath, args...)
	// own process group so oracled can signal the whole group on stop, and inherit oracled's env
	// (GHOST_LOG_LEVEL etc.). stdout/stderr inherited so llama-server's own logs land in oracled's log.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start llama-server: %w", err)
	}
	b.proc = cmd.Process
	return b.waitHealthy(ctx, 90*time.Second)
}

func (b *llamaBackend) waitHealthy(ctx context.Context, within time.Duration) error {
	deadline := time.Now().Add(within)
	url := "http://" + b.addr + "/health"
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("llama-server not healthy within %s", within)
}

// Infer sends one completion request to the private llama-server. This is the ONLY place the model's
// address is used. Text-only requests keep the native /completion path unchanged; requests carrying
// images go through /v1/chat/completions with data-URI content parts, which is the multimodal path
// the current llama-server (libmtmd, the mmproj we load) supports. Image paths must be ON THE VOLUME
// , this reads them and never sends bytes anywhere but loopback.
func (b *llamaBackend) Infer(ctx context.Context, req oracle.Request) (oracle.Response, error) {
	if len(req.Images) > 0 {
		return b.inferMultimodal(ctx, req)
	}
	payload := map[string]any{"prompt": req.Input}
	if req.MaxTokens > 0 {
		payload["n_predict"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+b.addr+"/completion", bytes.NewReader(body))
	if err != nil {
		return oracle.Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(httpReq)
	if err != nil {
		return oracle.Response{}, err
	}
	defer resp.Body.Close()
	var out struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return oracle.Response{}, err
	}
	return oracle.Response{Output: out.Content, Model: b.cfg.ModelName}, nil
}

// inferMultimodal builds an OpenAI-style chat completion with image content parts.
func (b *llamaBackend) inferMultimodal(ctx context.Context, req oracle.Request) (oracle.Response, error) {
	content := []map[string]any{{"type": "text", "text": req.Input}}
	for _, imgPath := range req.Images {
		raw, err := os.ReadFile(imgPath)
		if err != nil {
			return oracle.Response{}, fmt.Errorf("read image: %w", err)
		}
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]string{"url": dataURI(raw)},
		})
	}
	payload := map[string]any{
		"messages": []map[string]any{{"role": "user", "content": content}},
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+b.addr+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return oracle.Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(httpReq)
	if err != nil {
		return oracle.Response{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return oracle.Response{}, fmt.Errorf("chat/completions: http %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return oracle.Response{}, err
	}
	if len(out.Choices) == 0 {
		return oracle.Response{}, fmt.Errorf("chat/completions: empty choices")
	}
	return oracle.Response{Output: out.Choices[0].Message.Content, Model: b.cfg.ModelName}, nil
}

// dataURI wraps image bytes as a data URI, sniffing jpeg/png/webp by magic bytes (jpeg default).
func dataURI(raw []byte) string {
	mime := "image/jpeg"
	switch {
	case len(raw) > 8 && raw[0] == 0x89 && raw[1] == 'P' && raw[2] == 'N' && raw[3] == 'G':
		mime = "image/png"
	case len(raw) > 12 && string(raw[0:4]) == "RIFF" && string(raw[8:12]) == "WEBP":
		mime = "image/webp"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

// Stop signals llama-server (TERM then KILL) and reaps it. Called on oracled shutdown, which is the
// lock path, so the model process dies with the mount.
func (b *llamaBackend) Stop() {
	if b.proc == nil {
		return
	}
	_ = b.proc.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _, _ = b.proc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = b.proc.Kill()
		<-done
	}
	b.proc = nil
}
