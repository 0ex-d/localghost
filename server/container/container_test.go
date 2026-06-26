package container

import "testing"

type fake struct {
	slot int
	size uint64
}

func (f fake) Slot() int         { return f.slot }
func (f fake) SizeBytes() uint64 { return f.size }

func TestEqualSizesPass(t *testing.T) {
	l := Layout{
		Spec: Spec{SizeBytes: 20 << 30},
		Slots: map[int]Container{
			0: fake{0, 20 << 30},
			1: fake{1, 20 << 30},
			2: fake{2, 20 << 30},
		},
	}
	if err := l.Verify(3); err != nil {
		t.Fatalf("equal sizes should pass: %v", err)
	}
}

func TestUnequalSizeFails(t *testing.T) {
	// The real account bigger than the decoys would reveal it; Verify must reject.
	l := Layout{
		Spec: Spec{SizeBytes: 20 << 30},
		Slots: map[int]Container{
			0: fake{0, 40 << 30}, // bigger!
			1: fake{1, 20 << 30},
			2: fake{2, 20 << 30},
		},
	}
	if err := l.Verify(3); err == nil {
		t.Fatal("a larger container must fail the deniability invariant")
	}
}

func TestGrowAllKeepsEqual(t *testing.T) {
	l := Layout{
		Spec:  Spec{SizeBytes: 20 << 30},
		Slots: map[int]Container{0: fake{0, 20 << 30}, 1: fake{1, 20 << 30}},
	}
	// The grow callback takes only (slot, extra) , no key , proving lockstep growth is keyless.
	extra := map[int]uint64{}
	err := l.GrowAll(30<<30, func(slot int, e uint64) error { extra[slot] = e; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if extra[0] != 10<<30 || extra[1] != 10<<30 || l.Spec.SizeBytes != 30<<30 {
		t.Fatalf("GrowAll must append equal extra to every slot keylessly: %+v spec=%d", extra, l.Spec.SizeBytes)
	}
}
