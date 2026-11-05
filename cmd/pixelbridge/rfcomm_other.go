//go:build !linux

package main

import (
	"errors"
	"io"
)

func dialRFCOMM([6]byte, byte) (io.ReadWriteCloser, error) {
	return nil, errors.New("pixelbridge needs Linux; on a Mac use bridges/macos")
}
