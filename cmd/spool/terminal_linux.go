//go:build linux

package main

import (
	"syscall"
	"unsafe"
)

func terminalFD(fd uintptr) bool {
	var state syscall.Termios
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&state)))
	return err == 0
}

func pollInput(fd int) (bool, error) {
	var set syscall.FdSet
	if fd < 0 || fd >= len(set.Bits)*64 {
		return false, syscall.EINVAL
	}
	set.Bits[fd/64] |= 1 << uint(fd%64)
	timeout := syscall.NsecToTimeval(100000000)
	n, err := syscall.Select(fd+1, &set, nil, nil, &timeout)
	return n > 0, err
}
