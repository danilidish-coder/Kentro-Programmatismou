//go:build windows

package engine

import (
	"fmt"
	"syscall"
	"unsafe"
)

func atomicReplace(source, destination string) error {
	p := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")
	a := syscall.StringToUTF16Ptr(source)
	b := syscall.StringToUTF16Ptr(destination)
	r, _, e := p.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 0x1|0x8)
	if r == 0 {
		return fmt.Errorf("αποτυχία ασφαλούς αντικατάστασης: %w", e)
	}
	return nil
}
