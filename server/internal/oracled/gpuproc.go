package oracled

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// THE GPU, ASKED DIRECTLY. oracled's verdict came from llama-server's own startup lines
// ("ggml_cuda_init: found 1 CUDA devices", "offloaded 49/49 layers to GPU"). The mirror's
// llama.cpp (v0.5.0, 29 Sep 2026) prints none of them at its default verbosity, so a model
// answering at 52 tokens a second on the 4070 was reported "on the CPU": searchd stretched every
// caption deadline to CPU speed, and synthd refused to plan web searches ("the model is on the
// CPU"), so a follow-up like "how about now?" was searched word for word. When the lines say
// nothing, the driver is asked which processes hold GPU memory; the child's pid among them is the
// answer, whatever the log format.

// nvidiaSMI is the tool asked; tests point it at a stand-in.
var nvidiaSMI = "nvidia-smi"

// gpuMiBOfPID returns the GPU memory (MiB) the process holds, and whether the driver answered.
// Bounded: a card that fell off the bus makes nvidia-smi hang, and oracled must not.
func gpuMiBOfPID(pid int) (float64, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, nvidiaSMI, "--query-compute-apps=pid,used_memory", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0, false
	}
	var mib float64
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, ",")
		if len(f) < 2 {
			continue
		}
		if p, err := strconv.Atoi(strings.TrimSpace(f[0])); err == nil && p == pid {
			v, _ := strconv.ParseFloat(strings.TrimSpace(f[1]), 64)
			mib += v
		}
	}
	return mib, true
}

// seenOnGPU records what the driver says when the log said nothing: the process holds mib of
// GPU memory. A log that did speak is left as it is.
func (b *engineInfoBox) seenOnGPU(mib float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.info.Backend == "cuda" && b.info.Offloaded != "" {
		return
	}
	b.info.Backend = "cuda"
	if b.info.GPUMiB == 0 {
		b.info.GPUMiB = mib
	}
	if len(b.info.Devices) == 0 {
		b.info.Devices = []string{"a CUDA device (seen by nvidia-smi; this llama.cpp does not log its devices)"}
	}
}
