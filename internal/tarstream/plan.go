package tarstream

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
)

type planSegment struct {
	start, size   int64
	data          []byte
	file, version string
	parent        fs.FileInfo
}

// Plan is a seekable virtual tar. File bodies are opened only when read.
// The validator uses filesystem metadata, not content hashes.
type Plan struct {
	root         *os.Root
	ctx          context.Context
	segments     []planSegment
	size, pos    int64
	validator    string
	current      *os.File
	currentIndex int
}

func (p *Plan) Size() int64  { return p.size }
func (p *Plan) ETag() string { return p.validator }
func (p *Plan) Close() error {
	if p.current != nil {
		err := p.current.Close()
		p.current = nil
		return err
	}
	return nil
}

// PlanBounded plans the same depth as Estimate. A deeper tree or unsupported
// metadata returns complete=false, allowing the caller to use plain streaming.
func PlanBounded(ctx context.Context, root *os.Root, archiveRoot string, skip func(string) bool) (*Plan, bool, error) {
	archiveRoot = path.Base(path.Clean(archiveRoot))
	if archiveRoot == "." || archiveRoot == ".." || archiveRoot == "/" {
		archiveRoot = "archive"
	}
	p := &Plan{root: root, ctx: ctx, currentIndex: -1}
	hash := sha256.New()
	fmt.Fprintln(hash, "folder-tar-metadata-plan-v1")
	links := make(map[fileID]string)
	var walk func(*os.Root, string, int) (bool, error)
	walk = func(dir *os.Root, rel string, depth int) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		parent, err := dir.Stat(".")
		if err != nil {
			return false, err
		}
		ok, err := p.addHeader(hash, parent, path.Join(archiveRoot, rel)+"/", "", false, rel)
		if !ok || err != nil {
			return ok, err
		}
		entries, err := fs.ReadDir(dir.FS(), ".")
		if err != nil {
			return false, err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			child := path.Join(rel, entry.Name())
			if skip != nil && skip(child) {
				continue
			}
			info, err := dir.Lstat(entry.Name())
			if err != nil {
				return false, err
			}
			name := path.Join(archiveRoot, child)
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				target, err := dir.Readlink(entry.Name())
				if err != nil {
					return false, err
				}
				ok, err := p.addHeader(hash, info, name, target, false, child)
				if !ok || err != nil {
					return ok, err
				}
			case info.IsDir():
				if depth >= 1 {
					return false, nil
				}
				sub, err := OpenDirectory(dir, entry.Name(), info)
				if err != nil {
					return false, err
				}
				ok, err := walk(sub, child, depth+1)
				sub.Close()
				if !ok || err != nil {
					return ok, err
				}
			case info.Mode().IsRegular():
				id, linked := hardlinkID(info)
				target, duplicate := links[id]
				ok, err := p.addHeader(hash, info, name, target, linked && duplicate, child)
				if !ok || err != nil {
					return ok, err
				}
				if linked && duplicate {
					continue
				}
				if linked {
					links[id] = name
				}
				version, supported := metadataVersion(info)
				if !supported {
					return false, nil
				}
				if err := p.add(planSegment{size: info.Size(), file: child, version: version, parent: parent}); err != nil {
					return false, err
				}
				padding := (512 - info.Size()%512) % 512
				if err := p.add(planSegment{size: padding, data: make([]byte, padding)}); err != nil {
					return false, err
				}
			}
		}
		return true, nil
	}
	complete, err := walk(root, ".", 0)
	if err != nil || !complete {
		return nil, complete, err
	}
	if err := p.add(planSegment{size: 1024, data: make([]byte, 1024)}); err != nil {
		return nil, false, err
	}
	p.validator = fmt.Sprintf("\"%x\"", hash.Sum(nil))
	return p, true, nil
}

func (p *Plan) addHeader(hash hash.Hash, info fs.FileInfo, name, target string, hardlink bool, relative string) (bool, error) {
	version, ok := metadataVersion(info)
	if !ok {
		return false, nil
	}
	header, err := tar.FileInfoHeader(info, target)
	if err != nil {
		return false, err
	}
	header.Name = name
	if hardlink {
		header.Typeflag, header.Linkname, header.Size = tar.TypeLink, target, 0
	}
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	if err := writer.WriteHeader(header); err != nil {
		return false, err
	}
	fmt.Fprintf(hash, "%q %q\n", relative, version)
	hash.Write(buf.Bytes())
	return true, p.add(planSegment{size: int64(buf.Len()), data: buf.Bytes()})
}

func (p *Plan) add(s planSegment) error {
	if s.size < 0 || s.size > math.MaxInt64-p.size {
		return fmt.Errorf("archive size overflow")
	}
	if s.size == 0 {
		return nil
	}
	s.start = p.size
	p.size += s.size
	p.segments = append(p.segments, s)
	return nil
}

func (p *Plan) Seek(offset int64, whence int) (int64, error) {
	base := int64(0)
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = p.pos
	case io.SeekEnd:
		base = p.size
	default:
		return 0, fmt.Errorf("invalid whence")
	}
	if offset < -base || offset > math.MaxInt64-base {
		return 0, fmt.Errorf("invalid offset")
	}
	p.pos = base + offset
	return p.pos, nil
}

func (p *Plan) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	if p.pos >= p.size {
		return 0, io.EOF
	}
	i := sort.Search(len(p.segments), func(i int) bool { return p.segments[i].start+p.segments[i].size > p.pos })
	s := p.segments[i]
	offset := p.pos - s.start
	if int64(len(dst)) > s.size-offset {
		dst = dst[:int(s.size-offset)]
	}
	if s.file == "" {
		n := copy(dst, s.data[offset:])
		p.pos += int64(n)
		return n, nil
	}
	if p.current == nil || p.currentIndex != i {
		if err := p.Close(); err != nil {
			return 0, err
		}
		parent, name := path.Split(s.file)
		dir := p.root
		if parent != "" {
			parent = path.Clean(parent)
			info, err := p.root.Lstat(filepath.FromSlash(parent))
			if err != nil {
				return 0, err
			}
			if !info.IsDir() || !os.SameFile(info, s.parent) {
				return 0, fmt.Errorf("archive directory changed")
			}
			dir, err = OpenDirectory(p.root, filepath.FromSlash(parent), s.parent)
			if err != nil {
				return 0, err
			}
			defer dir.Close()
		}
		f, err := openFile(dir, name)
		if err != nil {
			return 0, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return 0, err
		}
		version, ok := metadataVersion(info)
		if !ok || !info.Mode().IsRegular() || version != s.version {
			f.Close()
			return 0, fmt.Errorf("archive file changed")
		}
		p.current, p.currentIndex = f, i
	}
	n, err := p.current.ReadAt(dst, offset)
	info, statErr := p.current.Stat()
	if statErr != nil {
		return 0, statErr
	}
	version, ok := metadataVersion(info)
	if !ok || version != s.version {
		return 0, fmt.Errorf("archive file changed during read")
	}
	p.pos += int64(n)
	return n, err
}
