package gateway

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/go-file-transfer/transfer-client/tcp"
	"example.com/go-file-transfer/transfer-client/udp"
)

// get sends one GET request through the router and returns the recorded response.
func get(router http.Handler, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestSuccessReturnsFileWithHeaders(t *testing.T) {
	var gotAddress, gotName string
	fetch := func(address string, filename string) ([]byte, error) {
		gotAddress, gotName = address, filename
		return []byte("hello"), nil
	}
	router := NewRouter("upstream:8080", fetch, 1)

	recorder := get(router, "/files?name=a.txt")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %v, want 200", recorder.Code)
	}
	if recorder.Body.String() != "hello" {
		t.Errorf("body = %q, want %q", recorder.Body.String(), "hello")
	}
	if !strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", recorder.Header().Get("Content-Type"))
	}
	if !strings.Contains(recorder.Header().Get("Content-Disposition"), `filename=a.txt`) {
		t.Errorf("Content-Disposition = %q", recorder.Header().Get("Content-Disposition"))
	}
	if recorder.Header().Get("Content-Length") != "5" {
		t.Errorf("Content-Length = %q, want 5", recorder.Header().Get("Content-Length"))
	}
	if gotAddress != "upstream:8080" || gotName != "a.txt" {
		t.Errorf("fetch called with (%q, %q)", gotAddress, gotName)
	}
}

func TestUnknownExtensionIsOctetStream(t *testing.T) {
	fetch := func(address string, filename string) ([]byte, error) { return []byte{1, 2}, nil }
	recorder := get(NewRouter("u", fetch, 1), "/files?name=data.unknownext")

	if recorder.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("Content-Type = %q", recorder.Header().Get("Content-Type"))
	}
}

func TestInvalidNamesAreRejectedWithoutCallingFetch(t *testing.T) {
	targets := []string{
		"/files",
		"/files?name=",
		"/files?name=..",
		"/files?name=a..b",
		"/files?name=a/b",
		"/files?name=..%2Fgo.mod",
		"/files?name=a%5Cb",
		"/files?name=a%20b",
		"/files?name=a%00b",
		"/files?name=" + strings.Repeat("x", maxNameLength+1),
	}
	called := false
	fetch := func(address string, filename string) ([]byte, error) {
		called = true
		return nil, nil
	}
	router := NewRouter("u", fetch, 1)

	for _, target := range targets {
		recorder := get(router, target)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%v: status = %v, want 400", target, recorder.Code)
		}
	}
	if called {
		t.Error("fetch was called for an invalid name")
	}
}

func TestErrorsMapToStatuses(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"file not found", &udp.ServerError{Message: "file-not-found"}, http.StatusNotFound},
		{"bad chunk id", &udp.ServerError{Message: "bad-chunk-id"}, http.StatusBadGateway},
		{"wrapped server error", fmt.Errorf("wrapped: %w", &udp.ServerError{Message: "file-not-found"}), http.StatusNotFound},
		{"tcp file not found", &tcp.ServerError{Message: "file-not-found"}, http.StatusNotFound},
		{"tcp bad chunk id", &tcp.ServerError{Message: "bad-chunk-id"}, http.StatusBadGateway},
		{"tcp no data", tcp.ErrNoData, http.StatusGatewayTimeout},
		{"tcp incomplete", fmt.Errorf("%w: [3]", tcp.ErrIncomplete), http.StatusGatewayTimeout},
		{"no data", udp.ErrNoData, http.StatusGatewayTimeout},
		{"incomplete", fmt.Errorf("%w: [3]", udp.ErrIncomplete), http.StatusGatewayTimeout},
		{"network error", fmt.Errorf("connecting to x: boom"), http.StatusGatewayTimeout},
	}
	for _, testCase := range cases {
		fetch := func(address string, filename string) ([]byte, error) { return nil, testCase.err }
		recorder := get(NewRouter("u", fetch, 1), "/files?name=a.txt")

		if recorder.Code != testCase.status {
			t.Errorf("%v: status = %v, want %v", testCase.name, recorder.Code, testCase.status)
		}
		if !strings.Contains(recorder.Body.String(), `"error"`) {
			t.Errorf("%v: body %q is not a JSON error", testCase.name, recorder.Body.String())
		}
	}
}

// The semaphore must let at most maxInFlight fetches run at once, but still let all
// requests finish.
func TestSemaphoreLimitsConcurrentFetches(t *testing.T) {
	const maxInFlight = 2
	const requests = 8

	var running, highestSeen atomic.Int32
	fetch := func(address string, filename string) ([]byte, error) {
		now := running.Add(1)
		for {
			highest := highestSeen.Load()
			if now <= highest || highestSeen.CompareAndSwap(highest, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return []byte("ok"), nil
	}
	router := NewRouter("u", fetch, maxInFlight)

	var group sync.WaitGroup
	statuses := make([]int, requests)
	for index := 0; index < requests; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			statuses[index] = get(router, "/files?name=a.txt").Code
		}()
	}
	group.Wait()

	for index, status := range statuses {
		if status != http.StatusOK {
			t.Errorf("request %v: status = %v, want 200", index, status)
		}
	}
	if highestSeen.Load() != maxInFlight {
		t.Errorf("highest concurrent fetches = %v, want %v", highestSeen.Load(), maxInFlight)
	}
}

func TestHealthReportsDraining(t *testing.T) {
	frontend := New("u", func(string, string) ([]byte, error) { return nil, nil }, 1, time.Second)

	if code := get(frontend.Router, "/health").Code; code != http.StatusOK {
		t.Errorf("health = %v, want 200", code)
	}
	frontend.StartDraining()
	if code := get(frontend.Router, "/health").Code; code != http.StatusServiceUnavailable {
		t.Errorf("health while draining = %v, want 503", code)
	}
}

func TestWaitIdle(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	fetch := func(string, string) ([]byte, error) {
		close(started)
		<-release
		return []byte("ok"), nil
	}
	frontend := New("u", fetch, 1, time.Second)
	go get(frontend.Router, "/files?name=a.txt")
	<-started

	if frontend.WaitIdle(30 * time.Millisecond) {
		t.Error("WaitIdle returned true while a download was running")
	}
	close(release)
	if !frontend.WaitIdle(2 * time.Second) {
		t.Error("WaitIdle returned false after the download finished")
	}
}

func TestBusyGatewayAnswers503WithRetryAfter(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	fetch := func(string, string) ([]byte, error) {
		close(started)
		<-release
		return []byte("ok"), nil
	}
	frontend := New("u", fetch, 1, 30*time.Millisecond)
	go get(frontend.Router, "/files?name=a.txt")
	<-started

	recorder := get(frontend.Router, "/files?name=b.txt")
	close(release)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %v, want 503", recorder.Code)
	}
	if recorder.Header().Get("Retry-After") != "1" {
		t.Errorf("Retry-After = %q, want 1", recorder.Header().Get("Retry-After"))
	}
}
