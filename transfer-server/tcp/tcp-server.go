package tcp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tcpdata "example.com/go-file-transfer/transfer-data"
	"example.com/go-file-transfer/transfer-server/health"
	"example.com/go-file-transfer/transfer-server/loss"
	"example.com/go-file-transfer/transfer-server/opts"
)

/*
 * TCP version of the FTP_6650 server. Many client connections at once, many requests on each:
 *   open <file>        -> every pack of the file, then "DONE <num_packs> "
 *   rety <file> <id>   -> pack <id> (numbered from 1), then "DONE <num_packs> "
 *   close              -> the server ends that connection
 * A pack is "<ID> <NUM_PACKS> <SIZE_OF_PACK> <64-bytes-of-data>", the same fields as the UDP
 * datagram. A failure is answered with "ERR <message> " (file-not-found, bad-chunk-id).
 *
 * optional args (each also has an environment variable, which the option overrides)
 *  -send-loss=N percent chance (0-100) that a pack the server sends is dropped. Defaults to 0.
 *  -recv-loss=N percent chance (0-100) that an open or rety the server receives is ignored. Defaults to 0.
 *  -port=N TCP port to listen on. Defaults to 8080. Env PORT.
 *  -health-port=N TCP port of the HTTP health check (GET /health) for a load balancer. Defaults to 8081. Env HEALTH_PORT.
 *  -drain=SECONDS time between the health check turning unhealthy and the server closing its port, so the load balancer stops sending connections first. Defaults to 5.
 *  -bucket=NAME S3 bucket files are fetched from for each request. Env BUCKET.
 *  -prefix=KEY/PREFIX/ key prefix prepended to every requested file name. Defaults to none. Env PREFIX.
 *  -dir=PATH serve files from a local folder instead of S3 (for development and tests). Env FILES_DIR.
 *  Exactly one of -bucket and -dir is required.
 *
 * TCP itself never loses data, so loss is simulated by the server skipping a pack or ignoring
 * a command. The client notices the gap from the pack numbers (or a silence) and asks again.
 * DONE, ERR and close are never dropped, so the exchange cannot get stuck.
 */

func printDebugMessage(message string) {
	fmt.Fprintf(os.Stderr, "DEBUG: %v\n", message)
}

// sendPack writes one pack as a single message, unless the simulated network loses it.
//
// Inputs: conn is the client connection; pack is the pack to send; sendLossPercent is the
// chance (0-100) that it is dropped.
// Returns: nil on success, and also nil when the pack is dropped on purpose, since a real
// sender cannot tell that a message was lost.
// Errors: a write error on the connection is returned.
// Side effects: writes to conn, or logs the drop to stderr instead.
func sendPack(conn net.Conn, pack tcpdata.UDP_Pack, sendLossPercent int) error {
	if loss.ShouldDrop(sendLossPercent) {
		printDebugMessage(fmt.Sprintf("Simulated loss: dropped outgoing pack %v of %v", pack.Id, pack.Num_Packs))
		return nil
	}
	// The data bytes need not be text, so build the message as a []byte and write it at once.
	message := fmt.Appendf(nil, "%d %d %d ", pack.Id, pack.Num_Packs, pack.Data.Size)
	message = append(message, pack.Data.Data[:]...)
	_, err := conn.Write(message)
	return err
}

// sendDone tells the client that the reply to its last request is complete.
func sendDone(conn net.Conn, numPacks int) error {
	_, err := fmt.Fprintf(conn, "DONE %d ", numPacks)
	return err
}

func sendError(conn net.Conn, message string) error {
	_, err := fmt.Fprintf(conn, "ERR %v ", message)
	return err
}

// Config is what a TCP server needs to answer clients.
type Config struct {
	// Source supplies the bytes of a requested file.
	Source opts.Source
	// SendLossPercent and RecvLossPercent (0-100) are the chances that an outgoing pack is
	// dropped and that an incoming open or rety is ignored.
	SendLossPercent int
	RecvLossPercent int
	// IdleTimeout is how long a connection may send nothing before the server closes it.
	IdleTimeout time.Duration
	// ShutdownWait is how long Serve waits for running connections once its listener is closed.
	ShutdownWait time.Duration
}

// Serve accepts connections on ln and serves each in its own goroutine, until ln is closed.
//
// Inputs: ln is the listening socket; config says where files come from and how much loss to
// simulate.
// Returns: after ln is closed and the running connections have finished, or ShutdownWait has
// passed.
// Errors: none returned. An accept failure other than a closed listener is logged and
// accepting continues.
// Side effects: starts one goroutine per connection, and logs to stderr.
func Serve(ln net.Listener, config Config) {
	var running sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			break
		}
		if err != nil {
			printDebugMessage("Error accepting: " + err.Error())
			time.Sleep(10 * time.Millisecond)
			continue
		}
		running.Add(1)
		go func() {
			defer running.Done()
			defer conn.Close()
			ServeConn(conn, config)
		}()
	}

	finished := make(chan struct{})
	go func() {
		running.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(config.ShutdownWait):
		printDebugMessage("Connections still open after the shutdown wait")
	}
}

// ServeConn answers open, rety and close commands from one client until it closes the
// connection, stays silent for IdleTimeout, or a write fails.
//
// Inputs: conn is the client connection; config as in Serve.
// Returns: when the session ends.
// Errors: none returned; failures end the session and are logged.
// Side effects: reads and writes conn, reads the requested file from the source, logs to
// stderr.
func ServeConn(conn net.Conn, config Config) {
	client := bufio.NewScanner(conn)
	client.Split(bufio.ScanWords)

	// scan reads the next word, giving the client IdleTimeout to send it.
	scan := func() bool {
		if config.IdleTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(config.IdleTimeout))
		}
		return client.Scan()
	}

	for scan() {
		command := client.Text()
		printDebugMessage(fmt.Sprintf("Processing command %v", command))

		var err error
		switch command {
		case "open":
			if !scan() {
				return
			}
			filename := client.Text()
			if loss.ShouldDrop(config.RecvLossPercent) {
				printDebugMessage("Simulated loss: ignored incoming open")
				continue
			}
			err = handleOpen(conn, config, filename)
		case "rety":
			if !scan() {
				return
			}
			filename := client.Text()
			if !scan() {
				return
			}
			idText := client.Text()
			if loss.ShouldDrop(config.RecvLossPercent) {
				printDebugMessage("Simulated loss: ignored incoming rety")
				continue
			}
			err = handleRetry(conn, config, filename, idText)
		case "close":
			printDebugMessage("Closing")
			return
		default:
			printDebugMessage(fmt.Sprintf("Received unknown command: %v", command))
		}
		if err != nil {
			printDebugMessage("Error writing to client: " + err.Error())
			return
		}
	}
}

// handleOpen sends every pack of the file once, then DONE, or ERR file-not-found.
func handleOpen(conn net.Conn, config Config, filename string) error {
	printDebugMessage(fmt.Sprintf("Processing open for file: \"%v\"", filename))

	file_data, err := config.Source.Fetch(filename)
	if err != nil {
		printDebugMessage("Error fetching file: " + err.Error())
		return sendError(conn, "file-not-found")
	}
	tcp_packs := tcpdata.ChunkBytes(file_data)

	for _, pack := range tcpdata.ReformatToUDPPacket(tcp_packs) {
		err := sendPack(conn, pack, config.SendLossPercent)
		if err != nil {
			return err
		}
	}
	printDebugMessage("Sending DONE")
	return sendDone(conn, len(tcp_packs))
}

// handleRetry sends only the pack of the file numbered id (starting from 1), then DONE, or
// ERR file-not-found, or ERR bad-chunk-id.
func handleRetry(conn net.Conn, config Config, filename string, idText string) error {
	printDebugMessage(fmt.Sprintf("Processing rety for file: \"%v\" pack %v", filename, idText))

	file_data, err := config.Source.Fetch(filename)
	if err != nil {
		printDebugMessage("Error fetching file: " + err.Error())
		return sendError(conn, "file-not-found")
	}
	tcp_packs := tcpdata.ChunkBytes(file_data)

	id, err := strconv.Atoi(idText)
	if err != nil || id < 1 || id > len(tcp_packs) {
		return sendError(conn, "bad-chunk-id")
	}

	err = sendPack(conn, tcpdata.ReformatToUDPPacket(tcp_packs)[id-1], config.SendLossPercent)
	if err != nil {
		return err
	}
	return sendDone(conn, len(tcp_packs))
}

// RunServer starts the TCP FTP_6650 server and serves until SIGTERM or SIGINT.
//
// Inputs: args may contain the options listed above; other arguments are ignored. Each of
// -port, -health-port, -bucket, -prefix and -dir falls back to its environment variable when
// the option is absent.
// Returns: after a SIGTERM or SIGINT, once the health check has reported unhealthy for the
// drain time and the running connections have finished (or 30 seconds have passed).
// Errors: ends the program through log.Fatal when a loss option is not an integer from 0 to
// 100, when a port or -drain is not a valid number, when not exactly one of -bucket and -dir
// is given or S3 cannot be configured, or when the server cannot listen on its ports.
// Side effects: opens a TCP socket and a TCP health port, prints the ports (and any
// simulated loss rates) to stdout, and serves connections with Serve.
func RunServer(args []string) {

	fmt.Fprintln(os.Stderr, "In the tcp version")

	port := opts.EnvOr("PORT", "8080")
	healthPort := opts.EnvOr("HEALTH_PORT", "8081")
	bucket := os.Getenv("BUCKET")
	prefix := os.Getenv("PREFIX")
	dir := os.Getenv("FILES_DIR")
	config := Config{IdleTimeout: 60 * time.Second, ShutdownWait: 30 * time.Second}
	drain := 5 * time.Second
	for _, option := range args {
		var err error
		if strings.HasPrefix(option, "-send-loss=") {
			config.SendLossPercent, err = loss.ParsePercent(option)
		} else if strings.HasPrefix(option, "-recv-loss=") {
			config.RecvLossPercent, err = loss.ParsePercent(option)
		} else if strings.HasPrefix(option, "-port=") {
			port = strings.TrimPrefix(option, "-port=")
		} else if strings.HasPrefix(option, "-health-port=") {
			healthPort = strings.TrimPrefix(option, "-health-port=")
		} else if strings.HasPrefix(option, "-bucket=") {
			bucket = strings.TrimPrefix(option, "-bucket=")
		} else if strings.HasPrefix(option, "-prefix=") {
			prefix = strings.TrimPrefix(option, "-prefix=")
		} else if strings.HasPrefix(option, "-dir=") {
			dir = strings.TrimPrefix(option, "-dir=")
		} else if strings.HasPrefix(option, "-drain=") {
			drain, err = opts.ParseSeconds(option)
		}
		if err != nil {
			log.Fatal(err)
		}
	}
	for _, value := range []string{port, healthPort} {
		err := opts.ValidPort(value)
		if err != nil {
			log.Fatal(err)
		}
	}

	source, err := opts.OpenSource(dir, bucket, prefix)
	if err != nil {
		log.Fatal(err)
	}
	config.Source = source

	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatal("Failed to listen")
	}

	var draining atomic.Bool
	go health.Serve(":"+healthPort, &draining)

	fmt.Printf("Listening on TCP port %v (health check on TCP port %v)\n", port, healthPort)
	if config.SendLossPercent > 0 || config.RecvLossPercent > 0 {
		fmt.Printf("Simulating loss: send %v%%, receive %v%%\n", config.SendLossPercent, config.RecvLossPercent)
	}

	// On SIGTERM (a scale-in or deploy) report unhealthy first so the load balancer stops
	// sending new connections, wait for it to notice, then close the listener. Serve returns
	// once the connections already open are done.
	signals, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	go func() {
		<-signals.Done()
		fmt.Printf("Shutting down: unhealthy now, stopping in %v\n", drain)
		draining.Store(true)
		time.Sleep(drain)
		ln.Close()
	}()

	Serve(ln, config)
}
