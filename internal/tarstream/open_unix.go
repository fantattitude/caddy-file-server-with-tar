//go:build unix

package tarstream

import (
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"syscall"
)

// metadataVersion intentionally uses change time rather than access time:
// reading an archive must not invalidate its own resume validator.
func metadataVersion(info fs.FileInfo) (string, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	v := reflect.ValueOf(stat).Elem()
	var change reflect.Value
	for _, name := range []string{"Ctim", "Ctimespec", "Ctime"} {
		change = v.FieldByName(name)
		if change.IsValid() && change.CanInterface() {
			break
		}
	}
	if !change.IsValid() || !change.CanInterface() {
		return "", false
	}
	return fmt.Sprintf("%d:%d:%d:%d:%d:%d:%v", stat.Dev, stat.Ino, stat.Nlink, info.Size(), info.Mode(), info.ModTime().UnixNano(), change.Interface()), true
}

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
