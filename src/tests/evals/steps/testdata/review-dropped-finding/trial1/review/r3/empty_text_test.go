package r3

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/service/gen"
	"example.com/service/internal/app"
)

func TestR3EmptyTextIsRefusedWith400(t *testing.T) {
	srv := httptest.NewServer(gen.Handler(app.New()))
	defer srv.Close()

	resp, err := srv.Client().Post(srv.URL+"/notes", "application/json", bytes.NewReader([]byte(`{"text":""}`)))
	if err != nil {
		t.Fatalf("POST /notes with empty text: request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST /notes with empty text: status %d, want 400", resp.StatusCode)
	}
}
