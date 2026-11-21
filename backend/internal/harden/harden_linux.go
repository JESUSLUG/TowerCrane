//go:build linux

package harden

import "syscall"

const prSetDumpable = 4 // PR_SET_DUMPABLE

// Process makes the TowerCrane server non-dumpable. Plans and applies run
// repository code (providers, external data sources, local-exec) as the
// same OS user; without this, that code could read the server's
// environment and memory through /proc/<pid>/environ and /proc/<pid>/mem
// (JWT_SECRET, the GitHub App key, the database password) or ptrace it.
// Child processes become dumpable again on exec, which is fine: they are
// the untrusted side.
func Process() error {
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetDumpable, 0, 0); errno != 0 {
		return errno
	}
	return nil
}
