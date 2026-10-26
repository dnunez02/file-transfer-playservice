package main

import (
	"os"

	"example.com/go-file-transfer/transfer-server/tcp"
	"example.com/go-file-transfer/transfer-server/udp"
)

func main() {
	if os.Args[1] == "udp" {
		udp.RunServer(os.Args[1:])
	} else {
		tcp.RunServer(os.Args[1:])
	}
}
