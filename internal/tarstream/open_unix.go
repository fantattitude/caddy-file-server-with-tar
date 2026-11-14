//go:build unix

package tarstream

import (
	"os"
	"syscall"
)

func openFile(root *os.Root, name string) (*os.File, error) {
	// A concurrent rename can replace a regular file with a symlink or FIFO.
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
