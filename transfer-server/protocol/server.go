package protocol

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	tcpdata "example.com/go-file-transfer/transfer-data"
)

func printDebugMessage(message string) {
	fmt.Fprintf(os.Stderr, "DEBUG: %v\n", message)
}

// FileSource supplies the bytes of a named file. Fetch must be safe for concurrent use.
type FileSource interface {
	Fetch(name string) ([]byte, error)
}

// job is one received message waiting for a worker to handle it.
type job struct {
	message string
	from    Peer
}

// Serve handles FTP_6650 messages from transport until it is closed, serving up to workers
// requests at the same time.
//
// One goroutine (the caller) reads messages and hands each to a pool of worker goroutines,
// so a slow transfer to one client no longer delays the others. Each worker runs
// handleCommand, which sends its replies with transport.Send.
//
// Inputs: source supplies file contents (S3 in the real server); transport delivers messages
// and sends replies; Receive is only ever called from the calling goroutine, while Send is
// called from several workers at once, so it must be safe for concurrent use. Once Receive
// returns net.ErrClosed, Send must still work until Serve returns, so running transfers
// can finish. workers is the number of requests handled in parallel and must
// be at least 1; 1 gives the old one-request-at-a-time behavior.
// Returns: once Receive reports net.ErrClosed (the transport was closed for shutdown), after
// the requests already queued or running have finished.
// Errors: any other failed Receive is logged and the loop keeps reading. When every worker
// is busy and the queue is full, the newly received message is dropped and logged, just as a
// real network drops datagrams when overloaded; the client's timeout and RETY logic
// cover it. Panics if workers is less than 1 (the queue size would be invalid).
// Side effects: starts workers goroutines that live until Serve returns, and logs to stderr.
func Serve(transport Transport, source FileSource, workers int) {
	// jobs is a queue, not a lock. The reader sends messages into it and every worker
	// receives from it, and Go guarantees each message goes to exactly one worker. No Mutex
	// is needed because handleCommand and the functions below it share no mutable state:
	// each call fetches the file from source and builds its own packets.
	jobs := make(chan job, workers*4)
	var running sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		running.Add(1)
		go func() {
			defer running.Done()
			for next := range jobs {
				handleCommand(transport, source, next.message, next.from)
			}
		}()
	}

	for {
		message, from, err := transport.Receive()
		if errors.Is(err, net.ErrClosed) {
			close(jobs)
			running.Wait()
			return
		}
		if err != nil {
			printDebugMessage("Error receiving: " + err.Error())
			continue
		}
		// A non-blocking send: if the queue is full we drop the message instead of
		// stalling the reader, which would bring back one-request-at-a-time behavior.
		select {
		case jobs <- job{message: message, from: from}:
		default:
			printDebugMessage("Overloaded: dropped incoming datagram")
		}
	}
}

// handleCommand parses one FTP_6650 message and runs the matching request.
//
// Inputs: transport is used to send replies; message is the text of the request (for
// example "OPEN a.txt"); from is the address replies go to.
// Returns: nothing.
// Errors: a request with too few arguments is answered with "ERR missing-argument". An
// empty message or an unknown command is logged and ignored, with no reply.
// Side effects: sends zero or more messages through transport and logs to stderr. Safe to
// call from several goroutines at once.
func handleCommand(transport Transport, source FileSource, message string, from Peer) {
	fields := strings.Fields(message)
	if len(fields) == 0 {
		return
	}
	command := fields[0]
	printDebugMessage(fmt.Sprintf("Processing command \"%v\"", command))

	switch command {
	case "OPEN":
		if len(fields) < 2 {
			sendError(transport, from, "missing-argument")
			return
		}
		handleOpen(transport, source, from, fields[1])
	case "RETY":
		if len(fields) < 3 {
			sendError(transport, from, "missing-argument")
			return
		}
		handleRetry(transport, source, from, fields[1], fields[2])
	default:
		printDebugMessage(fmt.Sprintf("Received unknown command: %v", command))
	}
}

// handleOpen fetches the file from source, sends every chunk once, or ERR file-not-found.
// The file is only held in local variables, so it is freed once the send finishes.
func handleOpen(transport Transport, source FileSource, to Peer, filename string) {
	printDebugMessage(fmt.Sprintf("Processing OPEN for file: \"%v\"", filename))

	file_data, err := source.Fetch(filename)
	if err != nil {
		printDebugMessage("Error fetching file: " + err.Error())
		sendError(transport, to, "file-not-found")
		return
	}
	tcp_packs := tcpdata.ChunkBytes(file_data)

	for _, pack := range tcpdata.ReformatToUDPPacket(tcp_packs) {
		err := sendPack(transport, to, pack)
		if err != nil {
			printDebugMessage("Error sending chunk: " + err.Error())
			return
		}
	}
}

// handleRetry sends only the chunk of the file numbered id (starting from 1).
// The file is fetched from source again and dropped after the chunk is sent.
func handleRetry(transport Transport, source FileSource, to Peer, filename string, idText string) {
	printDebugMessage(fmt.Sprintf("Processing RETY for file: \"%v\" chunk %v", filename, idText))

	file_data, err := source.Fetch(filename)
	if err != nil {
		printDebugMessage("Error fetching file: " + err.Error())
		sendError(transport, to, "file-not-found")
		return
	}
	tcp_packs := tcpdata.ChunkBytes(file_data)

	id, err := strconv.Atoi(idText)
	if err != nil || id < 1 || id > len(tcp_packs) {
		sendError(transport, to, "bad-chunk-id")
		return
	}

	err = sendPack(transport, to, tcpdata.ReformatToUDPPacket(tcp_packs)[id-1])
	if err != nil {
		printDebugMessage("Error sending chunk: " + err.Error())
	}
}

func sendError(transport Transport, to Peer, message string) {
	err := transport.Send(to, []byte("ERR "+message))
	if err != nil {
		printDebugMessage("Error sending ERR: " + err.Error())
	}
}

// sendPack writes a file datagram as one message: <ID> <NUM_CHUNKS> <SIZE_OF_CHUNK> <64-bytes-of-data>
func sendPack(transport Transport, to Peer, pack tcpdata.UDP_Pack) error {
	// The message is binary (the data bytes need not be valid text), so build it as a
	// []byte throughout. Appendf formats the header straight into a []byte, whereas
	// []byte(fmt.Sprintf(...)) would build a string and then copy it into a slice.
	message := fmt.Appendf(nil, "%d %d %d ", pack.Id, pack.Num_Packs, pack.Data.Size)
	message = append(message, pack.Data.Data[:]...)
	return transport.Send(to, message)
}
