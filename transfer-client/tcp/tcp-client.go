package tcp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	tcpdata "example.com/go-file-transfer/transfer-data"
)

// How many times open is sent when nothing comes back, how many rounds of rety requests the
// client makes before giving up, and the largest NUM_PACKS a message may announce (the same
// cap as the UDP client). readTimeout is how long the server may stay quiet before the client
// decides the round is over; it is a variable only so tests can shorten it.
const (
	maxOpenAttempts = 5
	maxRetryRounds  = 5
	maxChunks       = 65536
)

var readTimeout = 2 * time.Second

// ServerError is returned when the server answers a request with "ERR <message> ".
// Message holds the text after "ERR " (for example "file-not-found").
type ServerError struct {
	Message string
}

func (serverError *ServerError) Error() string {
	return "server error: " + serverError.Message
}

var (
	// ErrNoData means the server never answered open, even after maxOpenAttempts attempts.
	ErrNoData = errors.New("no answer from the server")
	// ErrIncomplete means packs were still missing after maxRetryRounds rounds of rety.
	ErrIncomplete = errors.New("gave up with packs still missing")
)

/*
 * required arg: file name. This is ALWAYS the last arg
 * optional args
 *. -host=XXX connects to XXX. If not supplied, defaults to "localhost".
 *  -port=YYY connects to port YYY. If not supplied, defaults to "8080".
 *
 * Packet loss is simulated by the server (-send-loss, -recv-loss); the client recovers by
 * asking again for the packs it is missing.
 */
func RunClient(args []string) {

	fmt.Fprintln(os.Stderr, "In the tcp version")

	filename := args[len(args)-1]
	host := "localhost"
	port := "8080"

	if len(args) > 1 {
		args = args[:len(args)-1]
		for _, option := range args {
			if strings.Contains(option, "-host=") {
				host = strings.Split(option, "=")[1]
			} else if strings.Contains(option, "-port=") {
				port = strings.Split(option, "=")[1]
			}
		}
	}

	file_data, err := Fetch(net.JoinHostPort(host, port), filename)
	if err != nil {
		log.Fatal(err)
	}

	err = os.WriteFile(filename, file_data, 0o644)
	if err != nil {
		log.Fatalf("Error in storing file: %v", err)
	}
}

// Fetch runs one TCP FTP_6650 download and returns the file's bytes. It sends open, collects
// the packs until the server says DONE, asks for any missing packs with rety on the same
// connection, and recombines the packs. It never exits the program and never touches the
// disk.
//
// Inputs: address is the server as "host:port"; filename is the file to request. The name
// must not contain spaces, since the protocol separates fields with spaces.
// Returns: the complete contents of the file (empty for an empty file).
// Errors: a *ServerError when the server replies "ERR <message> "; ErrNoData when the server
// never answers open (the command may have been dropped, so open is sent up to
// maxOpenAttempts times); an error wrapping ErrIncomplete (listing the missing IDs) when packs
// are still missing after maxRetryRounds rounds; and wrapped network errors when the
// connection fails or a message is cut off.
// Side effects: opens and closes a TCP connection, sets read deadlines on it, and logs
// progress to stderr.
func Fetch(address string, filename string) ([]byte, error) {
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connecting to %v: %w", address, err)
	}
	defer conn.Close()
	defer fmt.Fprintf(conn, "close\n") // runs before conn.Close

	reader := bufio.NewReader(conn)
	chunks := make(map[int]tcpdata.TCP_Pack)

	num_chunks := -1
	for attempt := 0; attempt < maxOpenAttempts && num_chunks < 0; attempt++ {
		_, err = fmt.Fprintf(conn, "open %v\n", filename)
		if err != nil {
			return nil, fmt.Errorf("sending open: %w", err)
		}
		num_chunks, err = readReplies(conn, reader, chunks, 1)
		if err != nil {
			return nil, err
		}
	}
	if num_chunks < 0 {
		return nil, ErrNoData
	}

	for round := 0; round < maxRetryRounds; round++ {
		missing := missingChunkIds(chunks, num_chunks)
		if len(missing) == 0 {
			break
		}
		fmt.Fprintf(os.Stderr, "Missing packs %v, retrying\n", missing)
		for _, id := range missing {
			_, err = fmt.Fprintf(conn, "rety %v %v\n", filename, id)
			if err != nil {
				return nil, fmt.Errorf("sending rety: %w", err)
			}
		}
		_, err = readReplies(conn, reader, chunks, len(missing))
		if err != nil {
			return nil, err
		}
	}

	missing := missingChunkIds(chunks, num_chunks)
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %v", ErrIncomplete, missing)
	}

	packs := make([]tcpdata.TCP_Pack, num_chunks)
	for id := 1; id <= num_chunks; id++ {
		packs[id-1] = chunks[id]
	}
	return tcpdata.RecombineData(packs), nil
}

// readReplies collects packs from the server until it has seen wantDone DONE messages, or
// the server has been quiet for readTimeout (a dropped command gets no reply at all).
//
// Inputs: conn and reader are the connection and its buffered reader; chunks is filled in
// place, keyed by pack ID (starting from 1); wantDone is how many DONE messages end the wait.
// Returns: the total number of packs the server announced in the last pack or DONE message,
// or -1 if it announced nothing.
// Errors: an "ERR <message> " reply is returned as a *ServerError, and so is any failure
// that is not a quiet timeout between messages. Malformed packs are an error too, since
// TCP gives no way to find the next message.
// Side effects: sets a read deadline on conn before every message, and mutates chunks.
func readReplies(conn net.Conn, reader *bufio.Reader, chunks map[int]tcpdata.TCP_Pack, wantDone int) (int, error) {
	total := -1
	done := 0
	for done < wantDone {
		conn.SetReadDeadline(time.Now().Add(readTimeout))
		word, err := reader.ReadString(' ')
		if err != nil {
			if isTimeout(err) && word == "" {
				return total, nil
			}
			return total, fmt.Errorf("reading reply: %w", err)
		}

		switch word = strings.TrimSpace(word); word {
		case "DONE":
			text, err := readWord(conn, reader)
			if err != nil {
				return total, err
			}
			total, err = strconv.Atoi(text)
			if err != nil || total < 0 || total > maxChunks {
				return total, fmt.Errorf("bad DONE message: %q", text)
			}
			done++
		case "ERR":
			message, err := readWord(conn, reader)
			if err != nil {
				return total, err
			}
			return total, &ServerError{Message: message}
		default:
			announced, err := readPack(conn, reader, word, chunks)
			if err != nil {
				return total, err
			}
			total = announced
		}
	}
	return total, nil
}

// readPack reads the rest of a pack whose ID has already been read: "<NUM_PACKS> <SIZE> "
// followed by exactly 64 bytes of data, and stores it in chunks.
//
// Returns: the total number of packs in the file, as announced by the pack.
// Errors: an error when a field is missing or does not parse, a value is out of range
// (ID outside 1..NUM_PACKS, NUM_PACKS above maxChunks, SIZE outside 0..64), or the data is
// cut short.
func readPack(conn net.Conn, reader *bufio.Reader, idText string, chunks map[int]tcpdata.TCP_Pack) (int, error) {
	numberTexts := [2]string{}
	for index := range numberTexts {
		text, err := readWord(conn, reader)
		if err != nil {
			return 0, err
		}
		numberTexts[index] = text
	}
	id, idErr := strconv.Atoi(idText)
	num_chunks, numErr := strconv.Atoi(numberTexts[0])
	size, sizeErr := strconv.Atoi(numberTexts[1])
	if idErr != nil || numErr != nil || sizeErr != nil {
		return 0, fmt.Errorf("bad pack header: %q %q %q", idText, numberTexts[0], numberTexts[1])
	}
	if id < 1 || id > num_chunks || num_chunks > maxChunks || size < 0 || size > tcpdata.CHUNK_SIZE {
		return 0, fmt.Errorf("out of range values %v %v %v", id, num_chunks, size)
	}

	var pack tcpdata.TCP_Pack
	conn.SetReadDeadline(time.Now().Add(readTimeout))
	_, err := io.ReadFull(reader, pack.Data[:])
	if err != nil {
		return 0, fmt.Errorf("reading pack %v data: %w", id, err)
	}
	pack.Size = int64(size)
	chunks[id] = pack
	return num_chunks, nil
}

// readWord reads one space-terminated field in the middle of a message.
//
// Errors: a wrapped read error, including a timeout, since a message must arrive whole.
func readWord(conn net.Conn, reader *bufio.Reader) (string, error) {
	conn.SetReadDeadline(time.Now().Add(readTimeout))
	word, err := reader.ReadString(' ')
	if err != nil {
		return "", fmt.Errorf("reading message: %w", err)
	}
	return strings.TrimSpace(word), nil
}

// missingChunkIds lists the packs that have not been received yet: the IDs in 1..num_chunks
// that are not in chunks, in increasing order. The slice is empty when the file is complete.
func missingChunkIds(chunks map[int]tcpdata.TCP_Pack, num_chunks int) []int {
	missing := make([]int, 0)
	for id := 1; id <= num_chunks; id++ {
		_, found := chunks[id]
		if !found {
			missing = append(missing, id)
		}
	}
	return missing
}

// isTimeout reports whether err came from a network operation hitting its deadline.
func isTimeout(err error) bool {
	var net_err net.Error
	return errors.As(err, &net_err) && net_err.Timeout()
}
