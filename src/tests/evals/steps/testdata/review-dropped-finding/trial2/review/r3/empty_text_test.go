package r3

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/service/gen"
	"example.com/service/internal/app"
)

func TestR3EmptyTextIsRefused(t *testing.T) {
	srv := httptest.NewServer(gen.Handler(app.New()))
	defer srv.Close()
	resp, err := srv.Client().Post(srv.URL+"/notes", "application/json", strings.NewReader(`{"text":""}`))
	if err != nil {
		t.Fatalf("POST /notes with empty text: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
