package main

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

const (
	afBluetooth   = 31
	btprotoRFCOMM = 3
)

// sockaddrRC is struct sockaddr_rc; the kernel keeps the address least-significant byte first.
type sockaddrRC struct {
	family  uint16
	bdaddr  [6]byte
	channel uint8
	_       uint8
}

func dialRFCOMM(mac [6]byte, channel byte) (io.ReadWriteCloser, error) {
	fd, err := syscall.Socket(afBluetooth, syscall.SOCK_STREAM, btprotoRFCOMM)
	if err != nil {
		return nil, fmt.Errorf("rfcomm socket: %w", err)
	}
	sa := sockaddrRC{family: afBluetooth, channel: channel}
	for i := range 6 {
		sa.bdaddr[i] = mac[5-i]
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_CONNECT, uintptr(fd), uintptr(unsafe.Pointer(&sa)), unsafe.Sizeof(sa)); errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("rfcomm connect: %w", errno)
	}
	// Non-blocking from here, so the runtime poller can interrupt a read when the link is closed.
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), "rfcomm"), nil
}
