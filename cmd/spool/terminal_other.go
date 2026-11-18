//go:build !darwin && !linux

package main

import "errors"

func terminalFD(uintptr) bool { return false }

func pollInput(int) (bool, error) {
	return false, errors.New("interactive input requires macOS or Linux")
}
