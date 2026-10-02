//go:build !windows

package clientservices

import "syscall"

// Opening a peer-controlled FIFO must not block before we can reject its mode.
func nonblockingReadFlag() int { return syscall.O_NONBLOCK }
