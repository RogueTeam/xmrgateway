//go:build openbsd && (amd64 || arm64 || riscv64)

package sandbox

import (
	"syscall"
	"unsafe"
)

const Supported = true

//go:linkname syscall_syscall syscall.syscall
func syscall_syscall(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno)

var (
	libc_pledge_trampoline_addr uintptr
	libc_unveil_trampoline_addr uintptr
)

//go:cgo_import_dynamic libc_pledge pledge "libc.so"
//go:cgo_import_dynamic libc_unveil unveil "libc.so"

func call(fn uintptr, a, b *byte) error {
	if _, _, errno := syscall_syscall(fn, uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 0); errno != 0 {
		return errno
	}
	return nil
}

func Unveil(path, permissions string) error {
	p, err := syscall.BytePtrFromString(path)
	if err != nil {
		return err
	}
	perms, err := syscall.BytePtrFromString(permissions)
	if err != nil {
		return err
	}
	return call(libc_unveil_trampoline_addr, p, perms)
}

// Lock forbids further unveil calls.
func Lock() error {
	return call(libc_unveil_trampoline_addr, nil, nil)
}

func Pledge(promises string) error {
	p, err := syscall.BytePtrFromString(promises)
	if err != nil {
		return err
	}
	return call(libc_pledge_trampoline_addr, p, nil)
}
