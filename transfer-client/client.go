package main

import (
	"os"

	"example.com/go-file-transfer/transfer-client/tcp"
	"example.com/go-file-transfer/transfer-client/udp"
)

func main() {
	if os.Args[1] == "udp" {
		udp.RunClient(os.Args[1:])
	} else {
		tcp.RunClient(os.Args[1:])
	}
}
