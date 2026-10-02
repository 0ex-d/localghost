//go:build !linux

// Package harden holds the few process-level settings every LocalGhost daemon makes first. A box
// is Linux (prctl, a private mount namespace); elsewhere the tree only builds and tests.
package harden

// NoDump does nothing outside Linux: there is no prctl, and no box.
func NoDump() {}
