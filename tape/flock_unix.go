//go:build !windows

package tape

import "syscall"

func flockExclusive(fd int) error {
	return syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
}
