// Taken from "golang.org/x/crypto/ssh/terminal".

package main

import (
	"syscall"
	"unsafe"
)

// GetSize returns the dimensions of the given terminal.
func TerminalSize(fd int) (width, height int, err error) {
	var dimensions [4]uint16

	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&dimensions)), 0, 0, 0); err != 0 {
		return -1, -1, err
	}
	return int(dimensions[1]), int(dimensions[0]), nil
}
