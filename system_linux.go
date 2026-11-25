//go:build linux

package main

import (
	"fmt"
	"os"
	"syscall"
)

func systemValues(path string) (float64, uint64, uint64) {
	var u syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &u)
	var st syscall.Statfs_t
	syscall.Statfs(path, &st)
	var pages, rss uint64
	f, e := os.Open("/proc/self/statm")
	if e == nil {
		fmt.Fscan(f, &pages, &rss)
		f.Close()
	}
	return float64(u.Utime.Nano()+u.Stime.Nano()) / 1e9, rss * uint64(os.Getpagesize()), st.Bavail * uint64(st.Bsize)
}
