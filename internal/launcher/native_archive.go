package launcher

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func validNativeGuestDirectory(path string) bool {
	if filepath.Clean(path) != path || filepath.Dir(path) != "/tmp" {
		return false
	}
	name := filepath.Base(path)
	suffix := strings.TrimPrefix(name, "paseo-dock-assets.")
	if suffix == name || suffix == "" {
		return false
	}
	for _, r := range suffix {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

type nativeContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r nativeContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// The root handle pins the validated host plugin directory. Every relative
// lookup stays inside it even if the named source tree changes during transfer.
func writeNativeArchive(ctx context.Context, root *os.Root, source, name string, out io.Writer) error {
	if !filepath.IsLocal(source) || filepath.Clean(source) == "." {
		return errors.New("invalid relative plugin asset source")
	}
	partial := ""
	for _, part := range strings.Split(filepath.Clean(source), string(filepath.Separator)) {
		if err := ctx.Err(); err != nil {
			return err
		}
		partial = filepath.Join(partial, part)
		info, err := root.Lstat(partial)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("plugin assets must not contain symbolic links")
		}
	}
	if !strings.HasPrefix(name, "asset-") || name == "asset-" {
		return errors.New("invalid native archive name")
	}
	for _, r := range strings.TrimPrefix(name, "asset-") {
		if r < '0' || r > '9' {
			return errors.New("invalid native archive name")
		}
	}
	writer := tar.NewWriter(out)
	var size int64
	var visit func(*os.Root, string, string) error
	visit = func(parent *os.Root, relative, archiveName string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		before, err := parent.Lstat(relative)
		if err != nil {
			return err
		}
		if before.Mode()&os.ModeSymlink != 0 {
			return errors.New("plugin assets must not contain symbolic links")
		}
		if !before.IsDir() && !before.Mode().IsRegular() {
			return errors.New("plugin assets must contain regular files")
		}
		// Do not follow a replacement final symlink or block on a raced FIFO.
		file, err := parent.OpenFile(relative, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(before, info) || info.Mode() != before.Mode() {
			return errors.New("plugin asset changed during read")
		}
		size += info.Size()
		if size > 128*1024*1024 {
			return errors.New("plugin assets exceed size limit")
		}
		header := &tar.Header{Name: archiveName, Mode: int64(info.Mode().Perm()), Size: info.Size(), Typeflag: tar.TypeReg}
		if info.IsDir() {
			header.Typeflag = tar.TypeDir
			header.Size = 0
		}
		if err = writer.WriteHeader(header); err != nil {
			return err
		}
		if !info.IsDir() {
			_, err = io.CopyN(writer, nativeContextReader{ctx, file}, info.Size())
			return err
		}
		directory, err := parent.OpenRoot(relative)
		if err != nil {
			return err
		}
		defer directory.Close()
		pinned, err := directory.Stat(".")
		if err != nil {
			return err
		}
		if !os.SameFile(info, pinned) {
			return errors.New("plugin directory changed during read")
		}
		entries, err := file.ReadDir(-1)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Name() == "." || entry.Name() == ".." || strings.ContainsAny(entry.Name(), "/\\") {
				return fmt.Errorf("invalid plugin asset name")
			}
			if err = visit(directory, entry.Name(), archiveName+"/"+entry.Name()); err != nil {
				return err
			}
		}
		return nil
	}
	err := visit(root, source, name)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return writer.Close()
}
