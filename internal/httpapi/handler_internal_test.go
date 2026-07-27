package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSONHandlesAnUnencodableResponseWithoutPanicking(t *testing.T) {
	response := httptest.NewRecorder()
	writeJSON(response, http.StatusOK, make(chan int))

	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", response.Code)
	}
	if response.Header().Get("Content-Type") !=
		"application/json; charset=utf-8" {
		t.Errorf("content type = %q", response.Header().Get("Content-Type"))
	}
}
