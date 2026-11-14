//go:build unix

package tarstream

import (
	"io/fs"
	"os"
	"syscall"
)

func hardlinkID(info fs.FileInfo) (fileID, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink < 2 {
		return fileID{}, false
	}
	return fileID{uint64(stat.Dev), uint64(stat.Ino)}, true
}

func openFile(root *os.Root, name string) (*os.File, error) {
	// A concurrent rename can replace a regular file with a symlink or FIFO.
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
