package health

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHealthTurnsUnhealthyWhenDraining(t *testing.T) {
	var draining atomic.Bool
	handler := Handler(&draining)

	for _, want := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
		if recorder.Code != want {
			t.Errorf("status = %v, want %v", recorder.Code, want)
		}
		draining.Store(true)
	}
}
