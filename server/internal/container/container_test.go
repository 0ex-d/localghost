package container

import "testing"

// The equal-size / decoy machinery (Spec, Layout, Verify, GrowAll) was removed with the move to the
// single-account model. The container package now only defines the Mounter seam, which is exercised
// by the hw (dm-crypt) and secd integration tests against real hardware. This test just asserts the
// package compiles and the interface is present.
func TestMounterInterfacePresent(t *testing.T) {
	var _ Mounter // compile-time check that the interface exists
}
