package oracled

import (
	"os"
	"path/filepath"
	"testing"
)

// When llama-server's log says nothing about CUDA, the driver's list of compute processes decides:
// the child's pid holding memory means the GPU. The log, when it speaks, is not overridden.
func TestOnGPUFromTheDriverWhenTheLogIsSilent(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "nvidia-smi")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\necho '2934170, 248'\necho '4242, 7810'\n"), 0o755)
	old := nvidiaSMI
	nvidiaSMI = fake
	defer func() { nvidiaSMI = old }()

	if mib, ok := gpuMiBOfPID(4242); !ok || mib != 7810 {
		t.Fatalf("pid 4242: %v %v", mib, ok)
	}
	if mib, ok := gpuMiBOfPID(1); !ok || mib != 0 {
		t.Fatalf("a pid not on the GPU: %v %v", mib, ok)
	}

	b := newEngineInfoBox()
	if b.get().OnGPU() {
		t.Fatal("nothing seen yet")
	}
	b.seenOnGPU(7810)
	e := b.get()
	if !e.OnGPU() || e.GPUMiB != 7810 || len(e.Devices) != 1 {
		t.Fatalf("after the driver said so: %+v (%s)", e, e.Verdict())
	}

	// a log that did speak stays as it said
	c := newEngineInfoBox()
	c.info = EngineInfo{Backend: "cuda", Offloaded: "0/49", Devices: []string{"RTX"}}
	c.seenOnGPU(100)
	if c.get().OnGPU() {
		t.Fatal("the log said 0 layers offloaded; the driver's word must not override it")
	}

	nvidiaSMI = filepath.Join(dir, "missing")
	if _, ok := gpuMiBOfPID(4242); ok {
		t.Fatal("no nvidia-smi must read as no answer")
	}
}
