package udp

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func init() { readTimeout = 100 * time.Millisecond }

// testServer is a throwaway UDP server. handle is called for every request and returns the
// datagrams to reply with; sources records the source port of each request.
type testServer struct {
	conn    *net.UDPConn
	mu      sync.Mutex
	ports   []int
	handler func(request string, count int) [][]byte
}

func startTestServer(t *testing.T, handler func(request string, count int) [][]byte) *testServer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	server := &testServer{conn: conn, handler: handler}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 1024)
		count := 0
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			server.mu.Lock()
			server.ports = append(server.ports, from.Port)
			server.mu.Unlock()
			count++
			for _, reply := range handler(string(buf[:n]), count) {
				conn.WriteToUDP(reply, from)
			}
		}
	}()
	return server
}

func (server *testServer) sourcePorts() []int {
	server.mu.Lock()
	defer server.mu.Unlock()
	return append([]int(nil), server.ports...)
}

func (server *testServer) address() string { return server.conn.LocalAddr().String() }

func chunk(id int, total int, text string) []byte {
	data := make([]byte, 64)
	copy(data, text)
	return append([]byte(strings.Join([]string{itoa(id), itoa(total), itoa(len(text))}, " ")+" "), data...)
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestFetchRetriesOpenWhenFirstIsIgnored(t *testing.T) {
	server := startTestServer(t, func(request string, count int) [][]byte {
		if count == 1 {
			return nil
		}
		return [][]byte{chunk(1, 1, "hello")}
	})

	data, err := Fetch(server.address(), "a.txt")

	if err != nil || string(data) != "hello" {
		t.Fatalf("Fetch = %q, %v", data, err)
	}
	if ports := server.sourcePorts(); len(ports) != 2 || ports[0] == ports[1] {
		t.Errorf("source ports = %v, want 2 different ports (a new socket per attempt)", ports)
	}
}

func TestFetchGivesUpWithErrNoDataAfterMaxOpenAttempts(t *testing.T) {
	server := startTestServer(t, func(string, int) [][]byte { return nil })

	_, err := Fetch(server.address(), "a.txt")

	if !errors.Is(err, ErrNoData) {
		t.Errorf("err = %v, want ErrNoData", err)
	}
	if ports := server.sourcePorts(); len(ports) != maxOpenAttempts {
		t.Errorf("server saw %d requests, want %d", len(ports), maxOpenAttempts)
	}
}

func TestFetchDoesNotRetryServerError(t *testing.T) {
	server := startTestServer(t, func(string, int) [][]byte { return [][]byte{[]byte("ERR file-not-found")} })

	_, err := Fetch(server.address(), "a.txt")

	var serverError *ServerError
	if !errors.As(err, &serverError) {
		t.Fatalf("err = %v, want *ServerError", err)
	}
	if ports := server.sourcePorts(); len(ports) != 1 {
		t.Errorf("server saw %d requests, want 1", len(ports))
	}
}

func TestFetchRedialsWhenRetryRoundGetsNoReply(t *testing.T) {
	server := startTestServer(t, func(request string, count int) [][]byte {
		switch {
		case strings.HasPrefix(request, "OPEN"):
			return [][]byte{chunk(1, 2, strings.Repeat("a", 64))} // chunk 2 is "lost"
		case count == 2:
			return nil // first RETY gets no answer
		default:
			return [][]byte{chunk(2, 2, "two")}
		}
	})

	data, err := Fetch(server.address(), "a.txt")

	if err != nil || string(data) != strings.Repeat("a", 64)+"two" {
		t.Fatalf("Fetch = %q, %v", data, err)
	}
	if ports := server.sourcePorts(); len(ports) != 3 || ports[1] == ports[2] {
		t.Errorf("source ports = %v, want the second RETY from a new port", ports)
	}
}
