//go:build linux

package main

/*
#cgo LDFLAGS: -lsqlite3
#include <stdlib.h>
#include <sqlite3.h>
*/
import "C"
import (
	"errors"
	"unsafe"
)

type database struct{ h *C.sqlite3 }

func openDatabase(path string) (*database, error) {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	d := new(database)
	if C.sqlite3_open(p, &d.h) != C.SQLITE_OK {
		e := d.failure()
		d.close()
		return nil, e
	}
	return d, nil
}
func (d *database) failure() error { return errors.New(C.GoString(C.sqlite3_errmsg(d.h))) }
func (d *database) exec(q string) error {
	p := C.CString(q)
	defer C.free(unsafe.Pointer(p))
	if C.sqlite3_exec(d.h, p, nil, nil, nil) != C.SQLITE_OK {
		return d.failure()
	}
	return nil
}
func (d *database) read(query string) ([]byte, error) {
	q := C.CString(query)
	defer C.free(unsafe.Pointer(q))
	var s *C.sqlite3_stmt
	if C.sqlite3_prepare_v2(d.h, q, -1, &s, nil) != C.SQLITE_OK {
		return nil, d.failure()
	}
	defer C.sqlite3_finalize(s)
	switch C.sqlite3_step(s) {
	case C.SQLITE_ROW:
		return C.GoBytes(unsafe.Pointer(C.sqlite3_column_text(s, 0)), C.sqlite3_column_bytes(s, 0)), nil
	case C.SQLITE_DONE:
		return nil, nil
	default:
		return nil, d.failure()
	}
}
func (d *database) close() {
	if d.h != nil {
		C.sqlite3_close(d.h)
		d.h = nil
	}
}
