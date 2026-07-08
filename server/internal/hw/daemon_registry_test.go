package hw

import "testing"

// TestRegistryInternalConsistency locks the invariants that the three-list duplication used to risk
// getting wrong silently. If someone adds a daemon to the registry incorrectly, these fail loudly.
func TestRegistryInternalConsistency(t *testing.T) {
	seeded := map[string]bool{}
	for _, n := range SeededBinaries() {
		seeded[n] = true
	}

	// Every supervised daemon must be seeded , watchd cannot start a binary that was never copied to
	// the volume. This is the exact "supervised but not seeded" bug the abstraction removes.
	for name := range SupervisedDaemons() {
		if !seeded[name] {
			t.Errorf("supervised daemon %s is not in SeededBinaries , watchd would try to start a "+
				"binary that was never copied to the volume", name)
		}
	}

	// Every required binary must be seeded (else provision hard-fails on a binary it then would not copy).
	for name := range RequiredBinaries() {
		if !seeded[name] {
			t.Errorf("required binary %s is not seeded", name)
		}
	}

	// Every critical daemon must be supervised (a critical daemon nobody watches is meaningless).
	supervised := SupervisedDaemons()
	for _, name := range CriticalDaemons() {
		if _, ok := supervised[name]; !ok {
			t.Errorf("critical daemon %s is not supervised", name)
		}
	}
}

// TestRegistryPortsUnique: no two supervised daemons share a health port.
func TestRegistryPortsUnique(t *testing.T) {
	seen := map[int]string{}
	for name, port := range SupervisedDaemons() {
		if other, clash := seen[port]; clash {
			t.Errorf("port %d assigned to both %s and %s", port, other, name)
		}
		seen[port] = name
	}
}

// TestStartOrderCoversSupervised: the start order lists exactly the supervised set (no supervised
// daemon is left out of the ordering, which would relegate it to the random-iteration fallback).
func TestStartOrderCoversSupervised(t *testing.T) {
	order := StartOrder()
	inOrder := map[string]bool{}
	for _, n := range order {
		inOrder[n] = true
	}
	for name := range SupervisedDaemons() {
		if !inOrder[name] {
			t.Errorf("supervised daemon %s missing from StartOrder", name)
		}
	}
	if len(order) != len(SupervisedDaemons()) {
		t.Errorf("StartOrder has %d entries, supervised set has %d", len(order), len(SupervisedDaemons()))
	}
}

// TestWatchdSeededNotSupervised: ghost.watchd is copied to the volume but is not in the map it
// supervises (it is the supervisor). This encodes the one deliberate asymmetry.
func TestWatchdSeededNotSupervised(t *testing.T) {
	seeded := map[string]bool{}
	for _, n := range SeededBinaries() {
		seeded[n] = true
	}
	if !seeded["ghost.watchd"] {
		t.Error("ghost.watchd must be seeded to the volume")
	}
	if _, ok := SupervisedDaemons()["ghost.watchd"]; ok {
		t.Error("ghost.watchd must NOT be in the supervised set , it is the supervisor")
	}
}
