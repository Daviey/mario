//go:build !windows

package sshd

import "syscall"

// ownProcessGroup detaches the child into its own process group so the
// SSH connection ending (or the server being signalled) cannot end the
// game session. Unix only; the windows build ships a stub (mosh-server
// does not exist there and -mosh is refused before this is reached).
func ownProcessGroup() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

// reap blocks until the child exits (mosh-server's handshake parent
// double-forks; reaping the parent avoids a zombie without touching the
// forked session server, which sits in the new process group).
func reap(pid int) {
	var ws syscall.WaitStatus
	syscall.Wait4(pid, &ws, 0, nil)
}
