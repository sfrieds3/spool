//go:build darwin

package main

import (
	"syscall"
	"unsafe"
)

func terminalFD(fd uintptr) bool {
	var state syscall.Termios
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCGETA), uintptr(unsafe.Pointer(&state)))
	return err == 0
}

func pollInput(fd int) (bool, error) {
	var set syscall.FdSet
	if fd < 0 || fd >= len(set.Bits)*32 {
		return false, syscall.EINVAL
	}
	set.Bits[fd/32] |= 1 << uint(fd%32)
	timeout := syscall.NsecToTimeval(100000000)
	err := syscall.Select(fd+1, &set, nil, nil, &timeout)
	return set.Bits[fd/32]&(1<<uint(fd%32)) != 0, err
}
