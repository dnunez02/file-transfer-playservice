package tcp

import (
	"bufio"
	"bytes"
	"errors"
	"math/rand/v2"
	"net"
	"sync"
	"testing"
	"time"

	"example.com/go-file-transfer/transfer-server/loss"
	tcpserver "example.com/go-file-transfer/transfer-server/tcp"
)

func init() { readTimeout = 100 * time.Millisecond }

// dropFirst scripts loss.IntN so that the first n drop decisions are "drop" (when the loss
// percent is above 0) and every later one is "keep".
func dropFirst(t *testing.T, n int) {
	t.Helper()
	var mu sync.Mutex
	calls := 0
	loss.IntN = func(int) int {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls <= n {
			return 0
		}
		return 99
	}
	t.Cleanup(func() { loss.IntN = rand.IntN })
}

// memorySource serves files held in memory, keyed by name.
type memorySource map[string][]byte

func (source memorySource) Fetch(name string) ([]byte, error) {
	data, ok := source[name]
	if !ok {
		return nil, errors.New("not found")
	}
	return data, nil
}

// startServer runs the TCP server on a free port with the given files and loss, and returns
// its address. The server stops when the test ends.
func startServer(t *testing.T, files memorySource, sendLoss int, recvLoss int) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		tcpserver.Serve(ln, tcpserver.Config{
			Source: files, SendLossPercent: sendLoss, RecvLossPercent: recvLoss,
			IdleTimeout: 5 * time.Second, ShutdownWait: 5 * time.Second,
		})
		close(finished)
	}()
	t.Cleanup(func() {
		ln.Close()
		<-finished
	})
	return ln.Addr().String()
}

// testFile returns a name and a source holding a file of size bytes with distinct content.
func testFile(size int) (string, memorySource, []byte) {
	data := make([]byte, size)
	for index := range data {
		data[index] = byte(index*7 + 3)
	}
	return "f.bin", memorySource{"f.bin": data}, data
}

func TestFetchWithoutLoss(t *testing.T) {
	name, files, want := testFile(330) // 5 full packs and a short one
	got, err := Fetch(startServer(t, files, 0, 0), name)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("Fetch = %d bytes, %v; want the %d original bytes", len(got), err, len(want))
	}
}

func TestFetchRecoversDroppedPacksWithRety(t *testing.T) {
	name, files, want := testFile(330)
	dropFirst(t, 2) // the first two packs are lost
	got, err := Fetch(startServer(t, files, 50, 0), name)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("Fetch = %d bytes, %v; want the %d original bytes", len(got), err, len(want))
	}
}

func TestFetchResendsOpenWhenServerIgnoresIt(t *testing.T) {
	name, files, want := testFile(130)
	dropFirst(t, 1) // the first open is ignored
	got, err := Fetch(startServer(t, files, 0, 50), name)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("Fetch = %d bytes, %v; want the %d original bytes", len(got), err, len(want))
	}
}

func TestFetchGivesUpWhenEveryPackIsDropped(t *testing.T) {
	name, files, _ := testFile(130)
	loss.IntN = rand.IntN
	_, err := Fetch(startServer(t, files, 100, 0), name)
	if !errors.Is(err, ErrIncomplete) {
		t.Errorf("err = %v, want ErrIncomplete", err)
	}
}

func TestFetchGivesUpWhenEveryOpenIsIgnored(t *testing.T) {
	name, files, _ := testFile(130)
	_, err := Fetch(startServer(t, files, 0, 100), name)
	if !errors.Is(err, ErrNoData) {
		t.Errorf("err = %v, want ErrNoData", err)
	}
}

func TestFetchMissingFile(t *testing.T) {
	_, files, _ := testFile(10)
	_, err := Fetch(startServer(t, files, 0, 0), "nope")
	var serverError *ServerError
	if !errors.As(err, &serverError) || serverError.Message != "file-not-found" {
		t.Errorf("err = %v, want file-not-found", err)
	}
}

func TestFetchEmptyFile(t *testing.T) {
	name, files, _ := testFile(0)
	got, err := Fetch(startServer(t, files, 0, 0), name)
	if err != nil || len(got) != 0 {
		t.Errorf("Fetch = %d bytes, %v; want an empty file", len(got), err)
	}
}

func TestServerServesSeveralClientsAtOnce(t *testing.T) {
	name, files, want := testFile(1000)
	address := startServer(t, files, 0, 0)

	var group sync.WaitGroup
	results := make([]error, 6)
	for index := range results {
		group.Add(1)
		go func() {
			defer group.Done()
			got, err := Fetch(address, name)
			if err == nil && !bytes.Equal(got, want) {
				err = errors.New("wrong content")
			}
			results[index] = err
		}()
	}
	group.Wait()

	for index, err := range results {
		if err != nil {
			t.Errorf("client %d: %v", index, err)
		}
	}
}

func TestServeReturnsAfterListenerClosesAndClientsFinish(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	name, files, _ := testFile(100)
	finished := make(chan struct{})
	go func() {
		tcpserver.Serve(ln, tcpserver.Config{Source: files, IdleTimeout: 5 * time.Second, ShutdownWait: 5 * time.Second})
		close(finished)
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// Wait for a reply, so the server has certainly accepted this connection before the
	// listener closes (a connection still waiting to be accepted would be dropped).
	conn.Write([]byte("open " + name + "\n"))
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := bufio.NewReader(conn).ReadString(' '); err != nil {
		t.Fatal(err)
	}
	ln.Close()
	select {
	case <-finished:
		t.Fatal("Serve returned while a client was still connected")
	case <-time.After(200 * time.Millisecond):
	}
	conn.Write([]byte("close\n"))
	conn.Close()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after the last client left")
	}
}
