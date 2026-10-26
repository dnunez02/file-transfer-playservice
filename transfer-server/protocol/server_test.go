package protocol

import (
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSource struct {
	mu    sync.Mutex
	files map[string][]byte
	calls int
}

func (f *fakeSource) Fetch(name string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	data, ok := f.files[name]
	if !ok {
		return nil, errors.New("not found")
	}
	return data, nil
}

type recordingTransport struct {
	mu   sync.Mutex
	sent []string
}

func (r *recordingTransport) Receive() (string, Peer, error) { select {} }
func (r *recordingTransport) Send(to Peer, message []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, string(message))
	return nil
}

var peer Peer = &net.UDPAddr{}

func TestOpenSendsEveryChunk(t *testing.T) {
	source := &fakeSource{files: map[string][]byte{"a.txt": []byte(strings.Repeat("x", 130))}}
	transport := &recordingTransport{}
	handleCommand(transport, source, "OPEN a.txt", peer)
	if len(transport.sent) != 3 {
		t.Fatalf("got %d datagrams, want 3", len(transport.sent))
	}
	if !strings.HasPrefix(transport.sent[2], "3 3 2 ") {
		t.Errorf("last datagram header = %q", transport.sent[2][:6])
	}
}

func TestOpenMissingFile(t *testing.T) {
	transport := &recordingTransport{}
	handleCommand(transport, &fakeSource{}, "OPEN nope", peer)
	if len(transport.sent) != 1 || transport.sent[0] != "ERR file-not-found" {
		t.Errorf("sent = %q", transport.sent)
	}
}

func TestRetrySendsOneChunkAndRefetches(t *testing.T) {
	source := &fakeSource{files: map[string][]byte{"a.txt": []byte(strings.Repeat("x", 130))}}
	transport := &recordingTransport{}
	handleCommand(transport, source, "OPEN a.txt", peer)
	handleCommand(transport, source, "RETY a.txt 2", peer)
	if source.calls != 2 {
		t.Errorf("fetch calls = %d, want 2 (nothing kept between requests)", source.calls)
	}
	if got := transport.sent[len(transport.sent)-1]; !strings.HasPrefix(got, "2 3 64 ") {
		t.Errorf("retry datagram header = %q", got[:7])
	}
}

func TestRetryBadChunkId(t *testing.T) {
	source := &fakeSource{files: map[string][]byte{"a.txt": []byte("hi")}}
	transport := &recordingTransport{}
	handleCommand(transport, source, "RETY a.txt 5", peer)
	if len(transport.sent) != 1 || transport.sent[0] != "ERR bad-chunk-id" {
		t.Errorf("sent = %q", transport.sent)
	}
}

// closableTransport delivers queued messages, then reports net.ErrClosed from Receive
// while Send keeps working.
type closableTransport struct {
	recordingTransport
	messages chan string
}

func (c *closableTransport) Receive() (string, Peer, error) {
	message, ok := <-c.messages
	if !ok {
		return "", nil, net.ErrClosed
	}
	return message, peer, nil
}

func TestServeReturnsAfterCloseAndFinishesRunningTransfers(t *testing.T) {
	source := &fakeSource{files: map[string][]byte{"a.txt": []byte(strings.Repeat("x", 640))}}
	transport := &closableTransport{messages: make(chan string, 4)}
	transport.messages <- "OPEN a.txt"
	transport.messages <- "OPEN a.txt"
	close(transport.messages)

	done := make(chan struct{})
	go func() {
		Serve(transport, source, 2)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after Receive reported net.ErrClosed")
	}
	if len(transport.sent) != 20 {
		t.Errorf("sent %d datagrams, want 20 (both transfers complete)", len(transport.sent))
	}
}
