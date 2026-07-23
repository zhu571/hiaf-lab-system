package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPAHandler(t *testing.T) {
	staticFS := fstest.MapFS{
		"index.html":    {Data: []byte("<main>app</main>")},
		"assets/app.js": {Data: []byte("export default 1")},
	}
	handler := spaHandler(fs.FS(staticFS))

	for _, tc := range []struct {
		path, contentType, body string
		status                  int
	}{
		{"/assets/app.js", "text/javascript", "export default 1", http.StatusOK},
		{"/projects/one", "text/html", "<main>app</main>", http.StatusOK},
		{"/assets/missing.js", "text/plain", "404 page not found", http.StatusNotFound},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != tc.status || !strings.HasPrefix(response.Header().Get("Content-Type"), tc.contentType) || !strings.Contains(response.Body.String(), tc.body) {
				t.Fatalf("got status=%d content-type=%q body=%q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
			}
		})
	}
}
