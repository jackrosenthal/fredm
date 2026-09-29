package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// VT ioctls from <linux/vt.h>, which golang.org/x/sys/unix does not define.
const (
	vtOpenQry   = 0x5600
	vtGetState  = 0x5603
	vtActivate  = 0x5606
	vtWaitActiv = 0x5607
)

// vtStat mirrors struct vt_stat.
type vtStat struct {
	active uint16
	signal uint16
	state  uint16
}

func vtIoctl(req, arg uintptr) error {
	f, err := os.OpenFile("/dev/tty0", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), req, arg)
	if errno != 0 {
		return errno
	}
	return nil
}

// activeVT returns the number of the VT in the foreground.
func activeVT() (int, error) {
	var st vtStat
	if err := vtIoctl(vtGetState, uintptr(unsafe.Pointer(&st))); err != nil {
		return 0, fmt.Errorf("VT_GETSTATE: %w", err)
	}
	return int(st.active), nil
}

// freeVT returns the first VT that no process has open.
func freeVT() (int, error) {
	var n int32
	if err := vtIoctl(vtOpenQry, uintptr(unsafe.Pointer(&n))); err != nil {
		return 0, fmt.Errorf("VT_OPENQRY: %w", err)
	}
	if n < 1 {
		return 0, fmt.Errorf("no free VT")
	}
	return int(n), nil
}

// switchVT brings VT n to the foreground and waits for the switch.
func switchVT(n int) error {
	if err := vtIoctl(vtActivate, uintptr(n)); err != nil {
		return fmt.Errorf("VT_ACTIVATE %d: %w", n, err)
	}
	if err := vtIoctl(vtWaitActiv, uintptr(n)); err != nil {
		return fmt.Errorf("VT_WAITACTIVE %d: %w", n, err)
	}
	return nil
}
