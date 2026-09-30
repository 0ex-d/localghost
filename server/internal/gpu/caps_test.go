package gpu

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The card's facts are read once, after the first good answer: the full field list, the CUDA
// version from the header, and the rows the drill-in shows.
func TestCapsReadOnceAfterAGoodAnswer(t *testing.T) {
	dir := t.TempDir()
	Reset()
	driverDir = filepath.Join(dir, "gpus")
	os.MkdirAll(filepath.Join(driverDir, "0000:2e:00.0"), 0o755)
	var calls func() int
	command, calls = fakeSMI(t, dir, `case "$1" in
  *memory.used*) echo '9064, 12282, 3' ;;
  *compute_cap*) echo 'NVIDIA GeForce RTX 4070, 8.9, 575.64.03, 12282 MiB, 200.00 W, 220.00 W, 3105 MHz, 10501 MHz, 4, 95.04.36.00.D1' ;;
  --query-gpu=*) exit 2 ;;
  *) echo '| NVIDIA-SMI 575.64.03   Driver Version: 575.64.03   CUDA Version: 12.9 |' ;;
esac`)
	if c := CapsNow(); !c.At.IsZero() {
		t.Fatal("caps known before any answer")
	}
	if _, err := Query(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := CapsNow()
	if c.Name != "NVIDIA GeForce RTX 4070" || c.Compute != "8.9" || c.CUDA != "12.9" || c.MemoryMiB != 12282 || c.PowerMax != "220.00 W" || c.PCIeGenMax != "4" {
		t.Fatalf("caps %+v", c)
	}
	rows := map[string]string{}
	for _, r := range c.Rows() {
		rows[r[0]] = r[1]
	}
	if rows["chip"] != "NVIDIA GeForce RTX 4070 · compute 8.9 (Ada Lovelace)" || rows["tensor maths"] != "FP16 · INT8 · BF16 · TF32 · FP8" ||
		rows["driver"] != "575.64.03 · CUDA 12.9" || rows["memory"] != "12.0 GB" || !strings.Contains(rows["power limit"], "card allows 220.00 W") {
		t.Fatalf("rows %v", rows)
	}
	if calls() != 1 {
		t.Fatalf("stats calls %d", calls())
	}
	// an older nvidia-smi that refuses the full list: the base fields
	Reset()
	command, _ = fakeSMI(t, dir, `case "$1" in
  *memory.used*) echo '100, 8192, 0' ;;
  *compute_cap*) echo 'Field "compute_cap" is not a valid field to query.' >&2; exit 2 ;;
  --query-gpu=*) echo 'Quadro P4000, 470.1, 8192 MiB, [N/A]' ;;
  *) exit 1 ;;
esac`)
	if _, err := Query(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := CapsNow(); c.Name != "Quadro P4000" || c.Compute != "" || c.PowerLimit != "" || c.CUDA != "" {
		t.Fatalf("base caps %+v", c)
	}
}

func TestArch(t *testing.T) {
	for _, c := range []struct{ in, arch, tensor string }{
		{"8.9", "Ada Lovelace", "FP16 · INT8 · BF16 · TF32 · FP8"},
		{"8.6", "Ampere", "FP16 · INT8 · BF16 · TF32"},
		{"7.5", "Turing", "FP16 · INT8"},
		{"6.1", "Pascal", "no tensor cores (the model runs on its CUDA cores, slower)"},
		{"12.0", "Blackwell", "FP16 · INT8 · BF16 · TF32 · FP8 · FP4"},
		{"", "", ""},
	} {
		if a, tn := Arch(c.in); a != c.arch || tn != c.tensor {
			t.Fatalf("%s: %q %q", c.in, a, tn)
		}
	}
}
