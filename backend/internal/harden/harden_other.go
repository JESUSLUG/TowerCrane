//go:build !linux

package harden

// Process is a no-op outside Linux.
func Process() error { return nil }
