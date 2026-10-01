//go:build linux

package deployment

import "syscall"

func RestrictCreation() { syscall.Umask(0o077) }
