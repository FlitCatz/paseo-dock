package launcher

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type nativeFailWriter struct{ err error }

func (w nativeFailWriter) Write([]byte) (int, error) { return 0, w.err }

func TestNativeArchivePinsHostRootAndPrivateModes(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "plugin")
	os.Mkdir(path, 0700)
	os.Mkdir(filepath.Join(path, "assets"), 0700)
	os.WriteFile(filepath.Join(path, "assets", "private"), []byte("PINNED_NATIVE_ASSET"), 0600)
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = os.Rename(path, path+"-moved"); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	os.Mkdir(filepath.Join(outside, "assets"), 0700)
	os.WriteFile(filepath.Join(outside, "assets", "private"), []byte("OUTSIDE_DATA"), 0600)
	if err = os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = writeNativeArchive(context.Background(), root, "assets", "asset-0", &out); err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(&out)
	dir, err := reader.Next()
	if err != nil || dir.Name != "asset-0" || dir.Mode != 0700 || dir.Typeflag != tar.TypeDir {
		t.Fatalf("private directory metadata: %+v %v", dir, err)
	}
	file, err := reader.Next()
	if err != nil || file.Name != "asset-0/private" || file.Mode != 0600 {
		t.Fatalf("private file metadata: %+v %v", file, err)
	}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "PINNED_NATIVE_ASSET" {
		t.Fatalf("root escaped: %q %v", data, err)
	}
	if _, err = reader.Next(); err != io.EOF {
		t.Fatal("extra archive entry", err)
	}
}

func TestNativeArchiveRejectsInvalidSourcesAndPropagatesErrors(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "assets"), 0700)
	os.WriteFile(filepath.Join(dir, "assets", "file"), []byte("PRIVATE"), 0600)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = os.Symlink("assets", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(t.TempDir(), filepath.Join(dir, "external")); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"../escape", "/absolute", "alias/file", "external", "."} {
		if err = writeNativeArchive(context.Background(), root, source, "asset-0", io.Discard); err == nil {
			t.Fatalf("invalid source accepted: %s", source)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = writeNativeArchive(ctx, root, "assets", "asset-0", io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	failure := errors.New("archive destination failed")
	if err = writeNativeArchive(context.Background(), root, "assets", "asset-0", nativeFailWriter{failure}); !errors.Is(err, failure) {
		t.Fatal("stream error lost", err)
	}
	sparse, err := os.Create(filepath.Join(dir, "large"))
	if err != nil {
		t.Fatal(err)
	}
	if err = sparse.Truncate(128*1024*1024 + 1); err != nil {
		t.Fatal(err)
	}
	sparse.Close()
	if err = writeNativeArchive(context.Background(), root, "large", "asset-0", io.Discard); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatal("size bound missing", err)
	}
}
