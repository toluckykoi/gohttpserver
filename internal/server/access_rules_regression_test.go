package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// M2 回归：makeIndex 整体替换目录大小缓存时，并发的目录列表请求不得触发数据竞争。
// 在 -race 下运行；裸变量赋值时代码会报 DATA RACE（makeIndex 写 vs historyDirSize 读）。
func TestDirSizeCacheNoRaceWithIndex(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		d := filepath.Join(root, "dir", string(rune('a'+i)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "f.bin"), make([]byte, 128), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := NewHTTPStaticServer(root, true)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				req := httptest.NewRequest("GET", "/?json=true", nil)
				rec := httptest.NewRecorder()
				s.ServeHTTP(rec, req)
			}
		}()
	}
	for i := 0; i < 50; i++ {
		if err := s.makeIndex(); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
}

// M1 回归：accessTables 的 allow:false 必须覆盖全部读取通道。
func TestAccessTablesBlocksAllReadPaths(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "block.file"), []byte("TOPSECRET-CONTENT"), 0o644)
	os.WriteFile(filepath.Join(root, "ok.file"), []byte("public"), 0o644)
	os.WriteFile(filepath.Join(root, ".ghs.yml"),
		[]byte("accessTables:\n- regex: block.file\n  allow: false\n"), 0o644)

	s := NewHTTPStaticServer(root, true)

	// 1) 列表隐藏
	req := httptest.NewRequest("GET", "/?json=true", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "block.file") {
		t.Error("listing exposes blocked file")
	}

	// 2) 直接下载被拒
	req = httptest.NewRequest("GET", "/block.file", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("direct GET of blocked file -> %d, want 4xx", rec.Code)
	}

	// 3) info（含哈希）被拒
	req = httptest.NewRequest("GET", "/block.file?op=info", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("info of blocked file -> %d, want 4xx", rec.Code)
	}

	// 4) 单文件打包被拒
	req = httptest.NewRequest("GET", "/block.file?op=archive", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code == http.StatusOK && strings.Contains(body, "TOPSECRET") {
		t.Error("single-file archive contains blocked content")
	}

	// 5) 目录打包剔除被拒文件
	req = httptest.NewRequest("GET", "/?op=archive", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "TOPSECRET") {
		t.Error("directory archive contains blocked content")
	}

	// 6) 多选打包被拒
	req = httptest.NewRequest("POST", "/-/zip", strings.NewReader(`{"paths":["/block.file"]}`))
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "TOPSECRET") {
		t.Error("zip-multi contains blocked content")
	}
}
