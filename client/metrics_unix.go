//go:build !windows

package client

import (
	"golang.org/x/sys/unix"
)

func getCPUSeconds() float64 {
	var rusage unix.Rusage
	_ = unix.Getrusage(unix.RUSAGE_SELF, &rusage)
	return float64(rusage.Utime.Sec) + float64(rusage.Utime.Usec)/1e6 +
		float64(rusage.Stime.Sec) + float64(rusage.Stime.Usec)/1e6
}
