//go:build windows

package sshd

import "syscall"

// mosh-server does not exist on Windows; -mosh is refused long before
// these are reached — the stubs only keep the package cross-compiling.
func ownProcessGroup() *syscall.SysProcAttr { return &syscall.SysProcAttr{} }

func reap(pid int) {}
