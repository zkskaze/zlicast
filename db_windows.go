//go:build windows

package main

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

var sqlite = syscall.NewLazyDLL("winsqlite3.dll")
var openSQL = sqlite.NewProc("sqlite3_open16")
var execSQL = sqlite.NewProc("sqlite3_exec")
var prepSQL = sqlite.NewProc("sqlite3_prepare_v2")
var stepSQL = sqlite.NewProc("sqlite3_step")
var finishSQL = sqlite.NewProc("sqlite3_finalize")
var textSQL = sqlite.NewProc("sqlite3_column_text")
var sizeSQL = sqlite.NewProc("sqlite3_column_bytes")
var closeSQL = sqlite.NewProc("sqlite3_close")
var messageSQL = sqlite.NewProc("sqlite3_errmsg16")
var kernel = syscall.NewLazyDLL("kernel32.dll")
var copyNative = kernel.NewProc("RtlMoveMemory")
var utf16Length = kernel.NewProc("lstrlenW")

type database struct{ h uintptr }

func (d *database) failure() error {
	if d.h == 0 {
		return errors.New("SQLite није доступан")
	}
	p, _, _ := messageSQL.Call(d.h)
	if p == 0 {
		return errors.New("SQLite грешка")
	}
	n, _, _ := utf16Length.Call(p)
	u := make([]uint16, min(n, 2048)+1)
	copyNative.Call(uintptr(unsafe.Pointer(&u[0])), p, (uintptr(len(u))-1)*2)
	return errors.New(syscall.UTF16ToString(u))
}
func openDatabase(path string) (*database, error) {
	if err := openSQL.Find(); err != nil {
		return nil, err
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	d := new(database)
	rc, _, _ := openSQL.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&d.h)))
	runtime.KeepAlive(p)
	if rc != 0 {
		err = d.failure()
		d.close()
		return nil, err
	}
	return d, nil
}
func (d *database) exec(q string) error {
	p, err := syscall.BytePtrFromString(q)
	if err != nil {
		return err
	}
	rc, _, _ := execSQL.Call(d.h, uintptr(unsafe.Pointer(p)), 0, 0, 0)
	runtime.KeepAlive(p)
	if rc != 0 {
		return d.failure()
	}
	return nil
}
func (d *database) read(query string) (b []byte, err error) {
	p, err := syscall.BytePtrFromString(query)
	if err != nil {
		return nil, err
	}
	var stmt uintptr
	rc, _, _ := prepSQL.Call(d.h, uintptr(unsafe.Pointer(p)), ^uintptr(0), uintptr(unsafe.Pointer(&stmt)), 0)
	runtime.KeepAlive(p)
	if rc != 0 {
		return nil, d.failure()
	}
	defer finishSQL.Call(stmt)
	rc, _, _ = stepSQL.Call(stmt)
	if rc == 101 {
		return nil, nil
	}
	if rc != 100 {
		return nil, d.failure()
	}
	ptr, _, _ := textSQL.Call(stmt, 0)
	n, _, _ := sizeSQL.Call(stmt, 0)
	if n == 0 {
		return nil, nil
	}
	b = make([]byte, n)
	copyNative.Call(uintptr(unsafe.Pointer(&b[0])), ptr, n)
	return b, nil
}
func (d *database) close() {
	if d.h != 0 {
		closeSQL.Call(d.h)
		d.h = 0
	}
}
