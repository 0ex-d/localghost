package gpu

// WHAT THE CARD CAN DO. The drill-in said whether the card answers; it did not say what it is. The
// static facts (the chip, its compute capability, its memory, the driver and CUDA it runs, its
// power limit and top clocks) are asked of nvidia-smi ONCE, after the shared probe has had a good
// answer (a card the driver cannot bring up is never poked for them), and kept for the life of the
// process. A field an older nvidia-smi does not know makes it refuse the whole query, so a refusal
// is retried with the fields every version has.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Caps is the card's static facts; empty strings where nvidia-smi had none.
type Caps struct {
	Name       string // NVIDIA GeForce RTX 4070
	Compute    string // 8.9
	Driver     string // 575.64.03
	CUDA       string // 12.9 (the highest the driver runs)
	MemoryMiB  float64
	PowerLimit string // 200.00 W
	PowerMax   string
	ClockSM    string // 3105 MHz
	ClockMem   string
	PCIeGenMax string // 4
	VBIOS      string
	At         time.Time
	Err        string // why they are not known
}

var (
	caps      Caps
	capsTried time.Time
	capsRetry = 30 * time.Minute // a failed caps query is not repeated sooner
)

var fullCapsFields = "name,compute_cap,driver_version,memory.total,power.limit,power.max_limit,clocks.max.sm,clocks.max.memory,pcie.link.gen.max,vbios_version"
var baseCapsFields = "name,driver_version,memory.total,power.limit"

// fetchCaps runs with mu held, after a good stats answer.
func fetchCaps(ctx context.Context) {
	if !caps.At.IsZero() || (!capsTried.IsZero() && time.Since(capsTried) < capsRetry) {
		return
	}
	capsTried = time.Now()
	c, err := queryCaps(ctx, fullCapsFields)
	if err != nil {
		c, err = queryCaps(ctx, baseCapsFields)
	}
	if err != nil {
		caps = Caps{Err: err.Error()}
		return
	}
	c.CUDA = cudaVersion(ctx)
	c.At = time.Now()
	caps = c
}

func queryCaps(ctx context.Context, fields string) (Caps, error) {
	cctx, cancel := context.WithTimeout(ctx, execBound)
	defer cancel()
	cmd := exec.CommandContext(cctx, command, "--query-gpu="+fields, "--format=csv,noheader")
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return Caps{}, fmt.Errorf("nvidia-smi caps: %v", err)
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	vals := strings.Split(line, ",")
	names := strings.Split(fields, ",")
	if len(vals) != len(names) {
		return Caps{}, fmt.Errorf("unparseable nvidia-smi caps line %q", line)
	}
	var c Caps
	for i, n := range names {
		v := strings.TrimSpace(vals[i])
		if v == "[N/A]" || v == "N/A" || strings.HasPrefix(v, "[Not Supported") {
			v = ""
		}
		switch n {
		case "name":
			c.Name = v
		case "compute_cap":
			c.Compute = v
		case "driver_version":
			c.Driver = v
		case "memory.total":
			c.MemoryMiB, _ = strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "MiB")), 64)
		case "power.limit":
			c.PowerLimit = v
		case "power.max_limit":
			c.PowerMax = v
		case "clocks.max.sm":
			c.ClockSM = v
		case "clocks.max.memory":
			c.ClockMem = v
		case "pcie.link.gen.max":
			c.PCIeGenMax = v
		case "vbios_version":
			c.VBIOS = v
		}
	}
	if c.Name == "" {
		return Caps{}, errors.New("nvidia-smi named no card")
	}
	return c, nil
}

var cudaRe = regexp.MustCompile(`CUDA Version:\s*([0-9.]+)`)

// cudaVersion is the "CUDA Version" nvidia-smi's own header prints (no query field carries it).
func cudaVersion(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, execBound)
	defer cancel()
	cmd := exec.CommandContext(cctx, command)
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	if m := cudaRe.FindStringSubmatch(string(out)); m != nil {
		return m[1]
	}
	return ""
}

// CapsNow is what is known of the card's facts (zero until the first good probe).
func CapsNow() Caps {
	mu.Lock()
	defer mu.Unlock()
	return caps
}

// Arch names the generation of a compute capability, and the maths its tensor cores do.
func Arch(compute string) (arch, tensor string) {
	major, minor := 0, 0
	if a, b, ok := strings.Cut(compute, "."); ok {
		major, _ = strconv.Atoi(a)
		minor, _ = strconv.Atoi(b)
	}
	switch {
	case major >= 12 || major == 10:
		arch = "Blackwell"
	case major == 9:
		arch = "Hopper"
	case major == 8 && minor == 9:
		arch = "Ada Lovelace"
	case major == 8:
		arch = "Ampere"
	case major == 7 && minor == 5:
		arch = "Turing"
	case major == 7:
		arch = "Volta"
	case major == 6:
		arch = "Pascal"
	case major > 0:
		arch = "older than Pascal"
	}
	var t []string
	v := major*10 + minor
	if v >= 70 {
		t = append(t, "FP16")
	}
	if v >= 75 {
		t = append(t, "INT8")
	}
	if v >= 80 {
		t = append(t, "BF16", "TF32")
	}
	if v >= 89 {
		t = append(t, "FP8")
	}
	if major >= 10 {
		t = append(t, "FP4")
	}
	if len(t) > 0 {
		tensor = strings.Join(t, " · ")
	} else if major > 0 {
		tensor = "no tensor cores (the model runs on its CUDA cores, slower)"
	}
	return arch, tensor
}

// CapsRows is the capabilities block of the drill-in; nothing when nothing is known yet.
func (c Caps) Rows() [][2]string {
	if c.At.IsZero() {
		if c.Err != "" {
			return [][2]string{{"capabilities", "not read: " + c.Err}}
		}
		return [][2]string{{"capabilities", "read after the first good answer from the card"}}
	}
	var rows [][2]string
	chip := c.Name
	arch, tensor := Arch(c.Compute)
	if c.Compute != "" {
		chip += " · compute " + c.Compute
		if arch != "" {
			chip += " (" + arch + ")"
		}
	}
	rows = append(rows, [2]string{"chip", chip})
	if tensor != "" {
		rows = append(rows, [2]string{"tensor maths", tensor})
	}
	if c.MemoryMiB > 0 {
		rows = append(rows, [2]string{"memory", fmt.Sprintf("%.1f GB", c.MemoryMiB/1024)})
	}
	drv := c.Driver
	if c.CUDA != "" {
		drv += " · CUDA " + c.CUDA
	}
	if drv != "" {
		rows = append(rows, [2]string{"driver", drv})
	}
	if c.PowerLimit != "" {
		p := c.PowerLimit
		if c.PowerMax != "" && c.PowerMax != c.PowerLimit {
			p += " (card allows " + c.PowerMax + ")"
		}
		rows = append(rows, [2]string{"power limit", p})
	}
	if c.ClockSM != "" {
		cl := c.ClockSM + " core"
		if c.ClockMem != "" {
			cl += " · " + c.ClockMem + " memory"
		}
		rows = append(rows, [2]string{"top clocks", cl})
	}
	if c.PCIeGenMax != "" {
		rows = append(rows, [2]string{"PCIe", "gen " + c.PCIeGenMax + " card"})
	}
	return rows
}
