//go:build !linux && !android && !windows

package service

// Platforms without an implemented memory probe use one automatic scan worker.
// Manual concurrency remains available; no guessed physical-memory value.
func availableMemory() uint64 { return 0 }
