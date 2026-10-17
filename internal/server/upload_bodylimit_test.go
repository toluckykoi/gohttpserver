package server

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

type countingBody struct {
	r *bytes.Reader
	n int64
}

func (c *countingBody) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
func (c *countingBody) Close() error { return nil }

// H3 回归：无上传权限的请求在鉴权阶段就应被拒绝，且**不得**先完整读取请求体。
// MaxBytesReader 置于 canUpload 之前后，计数应远小于实际 body 大小。
func TestUploadRejectedWithoutReadingFullBody(t *testing.T) {
	root := t.TempDir()
	s := NewHTTPStaticServer(root, true)
	s.Upload = false // 禁止上传：请求应被 403 拒绝

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "big.bin")
	chunk := make([]byte, 1<<20) // 1 MiB
	for i := 0; i < 8; i++ {     // 8 MiB 请求体
		fw.Write(chunk)
	}
	mw.Close()

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	body := &countingBody{r: bytes.NewReader(buf.Bytes())}
	req.Body = body
	req.ContentLength = int64(buf.Len())

	rec := httptest.NewRecorder()
	s.hUploadOrMkdir(rec, req)

	if rec.Code == 0 {
		t.Fatal("no response written")
	}
	// MaxBytesReader 包裹在鉴权之前：FormValue 读取会撞上限制（远小于 8 MiB）。
	if body.n > 33<<20 {
		t.Errorf("handler consumed %d bytes of body; MaxBytesReader should have cut it off much earlier", body.n)
	}
}
