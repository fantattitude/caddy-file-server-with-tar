package tarstream

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
)

// WriteDirectory writes a streaming archive of the specified local directory.
func WriteDirectory(ctx context.Context, w io.Writer, dir, archiveRoot string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return WriteRoot(ctx, w, root, archiveRoot, nil)
}

// WriteRoot archives the open directory without following symlink entries.
// The skip predicate receives paths relative to root, using slash separators.
func WriteRoot(ctx context.Context, w io.Writer, root *os.Root, archiveRoot string, skip func(string) bool) error {
	archiveRoot = path.Base(path.Clean(archiveRoot))
	if archiveRoot == "." || archiveRoot == ".." || archiveRoot == "/" {
		archiveRoot = "archive"
	}

	tw := tar.NewWriter(w)
	if err := walk(ctx, tw, root, ".", archiveRoot, skip); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("finishing tar stream: %w", err)
	}
	return nil
}

// OpenDirectory checks that name is a directory and pins it for subsequent
// relative opens. name is a single path component (or "." for the root).
func OpenDirectory(parent *os.Root, name string, before fs.FileInfo) (*os.Root, error) {
	dir, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := dir.Stat(".")
	if err != nil {
		dir.Close()
		return nil, err
	}
	if !os.SameFile(before, opened) {
		dir.Close()
		return nil, os.ErrNotExist
	}
	return dir, nil
}

func walk(ctx context.Context, tw *tar.Writer, dir *os.Root, relative, archiveRoot string, skip func(string) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := dir.Stat(".")
	if err != nil {
		return err
	}
	if err := writeHeader(tw, info, path.Join(archiveRoot, relative)+"/", ""); err != nil {
		return err
	}
	entries, err := fs.ReadDir(dir.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		child := path.Join(relative, name)
		if skip != nil && skip(child) {
			continue
		}
		if err := addEntry(ctx, tw, dir, name, child, archiveRoot, skip); err != nil {
			return err
		}
	}
	return nil
}

func addEntry(ctx context.Context, tw *tar.Writer, dir *os.Root, name, relative, archiveRoot string, skip func(string) bool) error {
	info, err := dir.Lstat(name)
	if err != nil {
		return err
	}
	archiveName := path.Join(archiveRoot, relative)
	switch mode := info.Mode(); {
	case mode&os.ModeSymlink != 0:
		target, err := dir.Readlink(name)
		if err != nil {
			return err
		}
		return writeHeader(tw, info, archiveName, target)
	case mode.IsDir():
		subdir, err := OpenDirectory(dir, name, info)
		if err != nil {
			return err
		}
		defer subdir.Close()
		return walk(ctx, tw, subdir, relative, archiveRoot, skip)
	case mode.IsRegular():
		return writeFile(ctx, tw, dir, name, archiveName, info)
	default:
		return nil
	}
}

func writeHeader(tw *tar.Writer, info fs.FileInfo, name, target string) error {
	header, err := tar.FileInfoHeader(info, target)
	if err != nil {
		return err
	}
	header.Name = name
	return tw.WriteHeader(header)
}

func writeFile(ctx context.Context, tw *tar.Writer, root *os.Root, name, archiveName string, before fs.FileInfo) error {
	f, err := openFile(root, name)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return fmt.Errorf("file changed while opening %q", name)
	}
	if err := writeHeader(tw, info, archiveName, ""); err != nil {
		return err
	}
	_, err = io.CopyN(tw, contextReader{ctx, f}, info.Size())
	return err
}

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}
