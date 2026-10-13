package launcher

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"paseo-sbx/internal/config"
	"path/filepath"
	"strings"
	"testing"
)

func nativeSymlinkedPrefixFixture(t *testing.T) (string, config.Entry, string) {
	t.Helper()
	runtime, _, entry, _ := launcherCategoryFixture(t, config.CategoryContainer)
	realHome := filepath.Join(t.TempDir(), "real-home")
	if err := os.Mkdir(realHome, 0700); err != nil {
		t.Fatal(err)
	}
	linkedHome := filepath.Join(t.TempDir(), "linked-home")
	if err := os.Symlink(realHome, linkedHome); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PASEO_HOME", linkedHome)
	root := filepath.Join(linkedHome, "plugin-data", "antigravity-cli")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	return runtime, entry, root
}

func TestNativeAssetArchiveAcceptsSymlinkedRootPrefix(t *testing.T) {
	runtime, entry, rawRoot := nativeSymlinkedPrefixFixture(t)
	asset := filepath.Join(rawRoot, "assets")
	if err := os.Mkdir(asset, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(asset, "file"), []byte("PRIVATE_ASSET"), 0600); err != nil {
		t.Fatal(err)
	}
	args, _, cleanup, err := prepareNativeContext(context.Background(), runtime, entry.Key(), []string{"--add-dir", asset}, entry.Workspace, entry, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if args[1] == asset || !strings.HasSuffix(args[1], "/asset-0") {
		t.Fatalf("asset path was not rewritten: %v", args)
	}
	rootPath, err := config.Canonical(rawRoot)
	if err != nil {
		t.Fatal(err)
	}
	source, err := nativeAssetRelativeSource(rootPath, asset)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var archive bytes.Buffer
	if err := writeNativeArchive(context.Background(), root, source, "asset-0", &archive); err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(&archive)
	if header, err := reader.Next(); err != nil || header.Name != "asset-0" {
		t.Fatalf("archive directory: %+v %v", header, err)
	}
	if header, err := reader.Next(); err != nil || header.Name != "asset-0/file" {
		t.Fatalf("archive file: %+v %v", header, err)
	}
	content, err := io.ReadAll(reader)
	if err != nil || string(content) != "PRIVATE_ASSET" {
		t.Fatalf("archive content: %q %v", content, err)
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatalf("extra archive entry: %v", err)
	}
}

func TestNativeAssetPathRejectsInTreeSymlinkWithSymlinkedPrefix(t *testing.T) {
	runtime, entry, root := nativeSymlinkedPrefixFixture(t)
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("PRIVATE_ASSET"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".", filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(root, "a", "file")
	_, _, _, err := prepareNativeContext(context.Background(), runtime, entry.Key(), []string{"--json-schema", asset}, entry.Workspace, entry, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "plugin assets must not contain symbolic links") {
		t.Fatalf("in-tree path symlink accepted: %v", err)
	}
}

func TestNativeAssetContentsRejectSymlinkWithSymlinkedPrefix(t *testing.T) {
	runtime, entry, root := nativeSymlinkedPrefixFixture(t)
	asset := filepath.Join(root, "assets")
	if err := os.Mkdir(asset, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../file", filepath.Join(asset, "link")); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := prepareNativeContext(context.Background(), runtime, entry.Key(), []string{"--add-dir", asset}, entry.Workspace, entry, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "plugin assets must not contain symbolic links") {
		t.Fatalf("in-tree content symlink accepted: %v", err)
	}
}
