package zipx

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"

	"testing"
)

// 第 8 轮审计回归：解压条目名曾借 Windows 路径规范化变体绕过 accessConfAliases /
// isServerStateAlias——".ghs.yml::$DATA"（ADS 流名直接写入主文件）与仅剥尾点未
// 覆盖的组合变体。归一化后全部拦截。

func buildRound8Zip(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "evil.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRound8UnzipSkipsProtectedNameVariants(t *testing.T) {
	dest := t.TempDir()
	origConf := "upload: true\n"
	origDB := "REAL-DB-CONTENT"
	for name, content := range map[string]string{
		".ghs.yml":        origConf,
		"gohttpserver.db": origDB,
	} {
		if err := os.WriteFile(filepath.Join(dest, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	zipPath := buildRound8Zip(t, map[string]string{
		".ghs.yml::$DATA":        "EVIL-CONF",
		".ghs.yml.":              "EVIL-CONF",
		".ghs.yml ":              "EVIL-CONF",
		".GHS.YML::$DATA":        "EVIL-CONF",
		"gohttpserver.db::$DATA": "EVIL-DB",
		"gohttpserver.db.":       "EVIL-DB",
		"normal.txt":             "ok",
	})

	if err := UnzipFile(context.Background(), zipPath, dest, nil); err != nil {
		t.Fatalf("UnzipFile: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dest, ".ghs.yml")); err != nil || string(got) != origConf {
		t.Errorf(".ghs.yml was tampered: got %q err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "gohttpserver.db")); err != nil || string(got) != origDB {
		t.Errorf("gohttpserver.db was tampered: got %q err=%v", got, err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "normal.txt"))
	if err != nil || string(got) != "ok" {
		t.Errorf("normal entry should extract fine: got %q err=%v", got, err)
	}
}

func TestRound8AliasFunctions(t *testing.T) {
	confVariants := []string{".ghs.yml", ".GHS.YML", "GHS~1.YML", ".ghs.yml.", ".ghs.yml ", ".ghs.yml::$DATA", ".ghs.yml.::$DATA"}
	for _, v := range confVariants {
		if !accessConfAliases(v) {
			t.Errorf("accessConfAliases(%q) = false, want true", v)
		}
	}
	stateVariants := []string{"gohttpserver.db", "GOHTTP~1.DB", "gohttpserver.db.", "gohttpserver.db ", "gohttpserver.db::$DATA", "frp-encryption.key::$DATA", "FRP-EN~1.KEY"}
	for _, v := range stateVariants {
		if !isServerStateAlias(v) {
			t.Errorf("isServerStateAlias(%q) = false, want true", v)
		}
	}
	for _, ok := range []string{"keep.txt", "notes.yml", "mydb.bak"} {
		if isServerStateAlias(ok) {
			t.Errorf("isServerStateAlias(%q) = true, want false", ok)
		}
		if accessConfAliases(ok) {
			t.Errorf("accessConfAliases(%q) = true, want false", ok)
		}
	}
}

func TestRound8RegisterServerStateName(t *testing.T) {
	RegisterServerStateName("round8-custom.db")
	for _, v := range []string{"round8-custom.db", "ROUND8-CUSTOM.DB.", "round8-custom.db::$DATA"} {
		if !isServerStateAlias(v) {
			t.Errorf("isServerStateAlias(%q) = false after registration, want true", v)
		}
	}
}
