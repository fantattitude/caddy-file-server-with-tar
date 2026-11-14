//go:build !unix

package tarstream

import (
	"errors"
	"os"
)

func openFile(_ *os.Root, _ string) (*os.File, error) {
	return nil, errors.New("tar downloads require a Unix filesystem")
}
