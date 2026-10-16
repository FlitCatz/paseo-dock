package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeAssetsAndBoundaries(t *testing.T) {
	home, _ := filepath.EvalSymlinks(t.TempDir())
	workspace, _ := filepath.EvalSymlinks(t.TempDir())
	assets := filepath.Join(home, "plugin-data", "antigravity-cli", "attachments", "session")
	os.MkdirAll(assets, 0700)
	os.WriteFile(filepath.Join(assets, "image.png"), []byte("image"), 0600)
	bin := t.TempDir()
	t.Setenv("PASEO_HOME", home)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	os.WriteFile(filepath.Join(bin, "sbx"), []byte("#!/bin/sh\nif [ \"$1\" = exec ] && [ \"$3\" = mktemp ]; then echo /tmp/paseo-dock-assets.test; fi\n"), 0700)
	args, paths, cleanup, err := prepareNative([]string{"--add-dir", workspace, "--add-dir", assets}, workspace, "box", &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if args[1] != workspace || args[3] != "/tmp/paseo-dock-assets.test/asset-0" {
		t.Fatal(args)
	}
	v := rewriteNativePaths(map[string]any{"text": "Read " + assets + "/image.png"}, paths).(map[string]any)
	if v["text"] != "Read /tmp/paseo-dock-assets.test/asset-0/image.png" {
		t.Fatal(v)
	}
	outside := t.TempDir()
	if _, _, _, err := prepareNative([]string{"--add-dir", outside}, workspace, "box", &strings.Builder{}); err == nil {
		t.Fatal("external directory accepted")
	}
	os.Symlink(filepath.Join(outside, "secret"), filepath.Join(assets, "secret"))
	if _, _, _, err := prepareNative([]string{"--add-dir", assets}, workspace, "box", &strings.Builder{}); err == nil {
		t.Fatal("symlink asset accepted")
	}
}
