//go:build windows

package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

func systemValues(path string) (float64, uint64, uint64) {
	k := syscall.NewLazyDLL("kernel32.dll")
	h, _, _ := k.NewProc("GetCurrentProcess").Call()
	var c, e, kern, user syscall.Filetime
	k.NewProc("GetProcessTimes").Call(h, uintptr(unsafe.Pointer(&c)), uintptr(unsafe.Pointer(&e)), uintptr(unsafe.Pointer(&kern)), uintptr(unsafe.Pointer(&user)))
	secs := float64((uint64(kern.HighDateTime)<<32|uint64(kern.LowDateTime))+(uint64(user.HighDateTime)<<32|uint64(user.LowDateTime))) / 1e7
	type mem struct {
		Size, Faults                                                                 uint32
		PeakWS, WorkingSet, PeakPaged, Paged, PeakNonPaged, NonPaged, Page, PeakPage uintptr
	}
	var x mem
	x.Size = uint32(unsafe.Sizeof(x))
	k.NewProc("K32GetProcessMemoryInfo").Call(h, uintptr(unsafe.Pointer(&x)), uintptr(x.Size))
	p, _ := syscall.UTF16PtrFromString(path)
	var free uint64
	k.NewProc("GetDiskFreeSpaceExW").Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), 0, 0)
	runtime.KeepAlive(p)
	return secs, uint64(x.WorkingSet), free
}
