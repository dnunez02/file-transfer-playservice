// Package gateway exposes the FTP_6650 UDP file server as an HTTP API: a GET request for a
// file is answered by running the FTP_6650 exchange against the UDP server on the caller's
// behalf.
package gateway

import (
	"errors"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"

	"example.com/go-file-transfer/transfer-client/tcp"
	"example.com/go-file-transfer/transfer-client/udp"
)

// maxNameLength is the longest file name the gateway accepts.
const maxNameLength = 255

// DefaultQueueWait is how long a request waits for a free transfer slot before the gateway
// answers 503, so an overloaded instance fails fast and the load balancer can try another.
const DefaultQueueWait = 5 * time.Second

// Fetcher downloads one file from an FTP_6650 server. Production code passes udp.Fetch or tcp.Fetch;
// tests pass a fake so the handler can be tested without a network.
type Fetcher func(address string, filename string) ([]byte, error)

// Gateway is the HTTP frontend: a Gin engine plus the state a graceful shutdown needs.
type Gateway struct {
	// Router serves GET /files?name=<filename> and GET /health.
	Router   *gin.Engine
	inflight chan struct{}
	draining atomic.Bool
}

// NewRouter builds the Gin engine with the default queue wait. See New.
func NewRouter(upstream string, fetch Fetcher, maxInFlight int) *gin.Engine {
	return New(upstream, fetch, maxInFlight, DefaultQueueWait).Router
}

// StartDraining makes GET /health answer 503 from now on, so a load balancer stops sending
// new requests. Requests already accepted keep running.
func (gateway *Gateway) StartDraining() {
	gateway.draining.Store(true)
}

// WaitIdle waits until no download is running.
//
// Inputs: timeout is the longest time to wait.
// Returns: true when no download was running before the timeout, false otherwise.
// Side effects: sleeps.
func (gateway *Gateway) WaitIdle(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for len(gateway.inflight) > 0 {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}

// New builds the gateway that serves GET /files?name=<filename>.
//
// Inputs: upstream is the FTP_6650 server as "host:port"; fetch downloads a file from it;
// maxInFlight is how many downloads may run against the upstream at the same time;
// queueWait is how long a request waits for one of those slots before getting 503.
// Returns: a ready-to-run *Gateway whose Router has request logging (except /health) and
// panic recovery.
// Errors: none at construction. Panics if maxInFlight is less than 1, because a channel
// with no capacity would block every request forever.
// Side effects: none until the returned router serves requests.
func New(upstream string, fetch Fetcher, maxInFlight int, queueWait time.Duration) *Gateway {
	// inflight is a counting semaphore: it lets at most cap(inflight) transfers run at once.
	//
	// If you know sync.Mutex: a Mutex is a semaphore with exactly one permit. Lock takes the
	// permit and Unlock gives it back. A semaphore with N permits lets N goroutines hold one
	// at the same time, and the (N+1)th has to wait for one of them to give theirs back.
	// If you know sync.RWMutex: that type distinguishes readers (many at once) from a writer
	// (alone). A semaphore makes no such distinction; every holder is the same kind, and N
	// is simply how many may be inside together.
	//
	// Go has no semaphore type in the standard library, so we use a buffered channel as the
	// permit counter. The buffer's capacity is N and its contents carry no information
	// (struct{} takes no memory):
	//
	//	acquire:  inflight <- struct{}{}   // blocks while the buffer is full (N holders)
	//	release:  <-inflight               // frees one slot for a waiting goroutine
	//
	// Unlike Mutex.Lock, the acquire can be combined with a select so a waiter can give up
	// when the HTTP client disconnects (ctx.Done()) instead of blocking forever.
	//
	// Two rules, the same as for a Mutex: always release exactly once for every acquire
	// (use defer right after a successful acquire), and never release without having acquired.
	// Unlike a Mutex, the permit is not owned by a goroutine, so any goroutine could release it;
	// the code here only ever releases in the goroutine that acquired.
	inflight := make(chan struct{}, maxInFlight)

	gateway := &Gateway{inflight: inflight}
	router := gin.New()
	// A load balancer polls /health every few seconds, so keep it out of the request log.
	router.Use(gin.LoggerWithConfig(gin.LoggerConfig{SkipPaths: []string{"/health"}}), gin.Recovery())
	router.GET("/files", handleGetFile(upstream, fetch, inflight, queueWait))
	router.GET("/health", gateway.handleHealthCheck())
	gateway.Router = router
	return gateway
}

// handleHealthCheck replies 200 normally and 503 once StartDraining has been called.
func (gateway *Gateway) handleHealthCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if gateway.draining.Load() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "draining"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

// handleGetFile builds the handler for GET /files?name=<filename>.
//
// Inputs: upstream and fetch are as in New; inflight is the semaphore described in
// New, shared by every request handled by the returned function; queueWait is how long a
// request waits for a permit.
// Returns: a gin.HandlerFunc. On success it replies 200 with the file's bytes, a Content-Type
// guessed from the extension (application/octet-stream if unknown), and a
// Content-Disposition attachment header. On failure it replies with a JSON body
// {"error": "..."} and the status from statusFor, or 400 when the name is invalid.
// Errors: none returned; every failure becomes an HTTP response. If the client disconnects
// while waiting for a permit, the handler gives up without calling fetch. If no permit frees up
// within queueWait it replies 503 with a Retry-After header, also without calling fetch.
// Side effects: each request waits for a semaphore permit, then opens a UDP socket to the
// upstream and exchanges datagrams with it (inside fetch).
func handleGetFile(upstream string, fetch Fetcher, inflight chan struct{}, queueWait time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Query("name")
		err := validateName(name)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Acquire a permit, or give up if the client leaves or the wait runs out.
		timer := time.NewTimer(queueWait)
		defer timer.Stop()
		select {
		case inflight <- struct{}{}:
			defer func() { <-inflight }() // release the permit when this request ends
		case <-c.Request.Context().Done():
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		case <-timer.C:
			c.Header("Retry-After", strconv.Itoa(1))
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the gateway is busy, try again"})
			return
		}

		file_data, err := fetch(upstream, name)
		if err != nil {
			c.Error(err)
			status, message := statusFor(err)
			c.JSON(status, gin.H{"error": message})
			return
		}

		content_type := mime.TypeByExtension(filepath.Ext(name))
		if content_type == "" {
			content_type = "application/octet-stream"
		}
		c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		c.Data(http.StatusOK, content_type, file_data)
	}
}

// validateName checks that name is safe and usable as an FTP_6650 file name.
//
// The UDP server opens the name relative to its working directory without any checks, so
// the gateway must keep path tricks such as "../../etc/passwd" from getting through. Names
// containing spaces are rejected as well, because the protocol separates fields by spaces
// and such a name could never be requested correctly.
//
// Inputs: name is the raw value of the "name" query parameter.
// Returns: nil when the name is acceptable.
// Errors: an error with a short user-facing message when name is empty, longer than
// maxNameLength, contains "/", "\" or "..", or contains whitespace or control characters.
// Side effects: none.
func validateName(name string) error {
	if name == "" {
		return errors.New("missing query parameter: name")
	}
	if len(name) > maxNameLength {
		return errors.New("name is too long")
	}
	for _, character := range name {
		if character == '/' || character == '\\' || unicode.IsSpace(character) || unicode.IsControl(character) {
			return errors.New("name contains a forbidden character")
		}
	}
	if containsDotDot(name) {
		return errors.New("name must not contain \"..\"")
	}
	return nil
}

// containsDotDot reports whether name contains two dots in a row.
//
// Inputs: name is the file name to inspect.
// Returns: true if ".." appears anywhere in name.
// Errors: none.
// Side effects: none.
func containsDotDot(name string) bool {
	for position := 0; position+1 < len(name); position++ {
		if name[position] == '.' && name[position+1] == '.' {
			return true
		}
	}
	return false
}

// statusFor translates a failure from the FTP_6650 client into an HTTP status and a short,
// safe message for the response body.
//
// Inputs: err is a non-nil error returned by a Fetcher.
// Returns: 404 when the server said "ERR file-not-found" (over UDP or TCP); 502 for any other ERR reply
// (the gateway or the server misbehaved); and 504 for everything else, meaning the server did
// not answer properly: no data, chunks still missing after retries, or a network error.
// Errors: none.
// Side effects: none.
func statusFor(err error) (int, string) {
	// The UDP and TCP clients each have their own ServerError type.
	var udpError *udp.ServerError
	var tcpError *tcp.ServerError
	message := ""
	if errors.As(err, &udpError) {
		message = udpError.Message
	} else if errors.As(err, &tcpError) {
		message = tcpError.Message
	}
	if message != "" {
		if message == "file-not-found" {
			return http.StatusNotFound, "file not found"
		}
		return http.StatusBadGateway, "the file server rejected the request"
	}
	return http.StatusGatewayTimeout, "the file server did not respond"
}
