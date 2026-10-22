package panel

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestConfigRequestSizeLimit(t *testing.T) {
	for _, size := range []int{1 << 20, (1 << 20) + 1} {
		called := false
		p := New(Config{APIKey: "test-key", SaveConfig: func(raw []byte) ([]string, error) {
			called = true
			if len(raw) != size {
				t.Fatalf("config was truncated: got %d bytes want %d", len(raw), size)
			}
			return nil, nil
		}})
		// Valid JSON followed by padding: truncation used to silently save it.
		req := httptest.NewRequest(http.MethodPost, "/panel/api/config", strings.NewReader("{}"+strings.Repeat(" ", size-2)))
		req.ContentLength = -1 // Unknown-length/chunked uploads must also be bounded.
		req.Header.Set("Authorization", "Bearer test-key")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if size == 1<<20 {
			if rec.Code != http.StatusOK || !called {
				t.Fatalf("at limit: code=%d saved=%t", rec.Code, called)
			}
		} else if rec.Code != http.StatusRequestEntityTooLarge || called {
			t.Fatalf("over limit: code=%d saved=%t", rec.Code, called)
		}
	}
}

func TestImportUploadLimitAndTempCleanup(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, tmp)
	}
	for _, size := range []int{2 << 20, 32 << 20, (32 << 20) + 1} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "accounts.json")
		if err != nil {
			t.Fatal(err)
		}
		// Produce an exactly sized whole request, including multipart framing.
		tailSize := len("\r\n--" + writer.Boundary() + "--\r\n")
		padding := size - body.Len() - tailSize - len("[{}]")
		if _, err := part.Write([]byte("[{}]" + strings.Repeat(" ", padding))); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if body.Len() != size {
			t.Fatalf("request size=%d want=%d", body.Len(), size)
		}
		req := httptest.NewRequest(http.MethodPost, "/panel/api/import/cockpit", &body)
		req.ContentLength = -1
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer test-key")
		rec := httptest.NewRecorder()
		newTestPanel().ServeHTTP(rec, req)
		want := http.StatusOK
		if size > 32<<20 {
			want = http.StatusRequestEntityTooLarge
		}
		if rec.Code != want {
			t.Fatalf("size=%d code=%d want=%d body=%s", size, rec.Code, want, rec.Body.String())
		}
		entries, err := os.ReadDir(tmp)
		if err != nil || len(entries) != 0 {
			t.Fatalf("upload left temporary credential files: count=%d err=%v", len(entries), err)
		}
	}
}
