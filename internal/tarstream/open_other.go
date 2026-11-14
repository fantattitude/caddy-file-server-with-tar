//go:build !unix

package tarstream

import (
	"errors"
	"io/fs"
	"os"
)

func hardlinkID(fs.FileInfo) (fileID, bool) { return fileID{}, false }

func openFile(_ *os.Root, _ string) (*os.File, error) {
	return nil, errors.New("tar downloads require a Unix filesystem")
}
