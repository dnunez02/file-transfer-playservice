package protocol

import "net"

// Peer is the network address a message came from and replies go to.
// *net.UDPAddr and *net.TCPAddr both satisfy it.
type Peer = net.Addr

// Transport moves whole FTP_6650 messages without the protocol knowing how.
//
// Concurrency contract: Serve calls Receive from a single goroutine only, but calls Send
// from many goroutines at once, so every implementation's Send must be safe for
// concurrent use.
type Transport interface {
	// Receive blocks until one message arrives.
	Receive() (message string, from Peer, err error)
	// Send delivers one message. It must be safe to call from multiple goroutines.
	Send(to Peer, message []byte) error
}
