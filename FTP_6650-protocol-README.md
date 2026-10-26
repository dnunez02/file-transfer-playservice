## The FTP_6650 Protocol

In this protocol, messages are sent **exactly as written here**, meaning no extra spaces or newlines should be inserted unless the message **explicitly** adds the space or newline.

### File Datagram Format

Before dictating the protocol, there is a key message format to understand: the _file datagram format_. This datagram format is how chunks of files are sent throughout the protocol. The datagram format is as follows:

```
<ID> <NUM_CHUNKS> <SIZE_OF_CHUNK> <64-bytes-of-data>
```
where
- NUM_CHUNKS if the number of chunks that constitute the entire file
- ID (starting from 1) dictates which numbered chunk this represents. ID can never exceed NUM_CHUNKS in a valid datagram.
- SIZE_OF_CHUNK is the number of bytes of the next 64 bytes are actual file data. This implies that _every datagram_ ends with exactly 64 bytes of data, even if most are garbage.

### Server Messages
The protocol FTP_6650 dictates the server must respond to the following messages:

`OPEN <filename>` : Received by the server to begin sending a file. When the
server receives this command, it checks if the file exists. This protocol assumes the file is stored in the same folder the server is run. If not, the server responds with `ERR file-not-found`. If the file does exist, the server responds breaks the file into chunks of 64 bytes and sends the each chunk _once_ using the file datagram format. Once all chunks have been sent, the server waits for its next message.

As an example, assume a file called `test.txt` has 130 bytes consisiting of 128 of the letter A followed by 1 of the letter B. When the server receives `OPEN test.txt`, the server would send the following messages with each line representing a single message.
```
1 3 64 <64 As as bytes>
2 3 64 <64 As as bytes>
3 3 2 <AB as bytes>
```

`RETY <filename> <ID>` : Received by the server to retry sending a particular chunk of a file. The server sends back the specific chunk for the given file in the file datagram format. The server then waits for its next message.

## Client Messages

The protocol FTP_6650 dictates the client can receive the following messages after they send an `OPEN` message:

`file datagram format` messages: Received by the client as a response to `OPEN` or `RETY`. The first time this message reaches the client (as a response to `OPEN`), the client begins storing the chunks for file reconstruction. The client continues to read messages assuming they fall in the file datagram format until no messages are received.

After no messages are received after a certain amount of time, the client stops reading and reviews the chunks. If the client identifies chunks are missing, the client sends a `RETY` message as stated previously. If the client has all of the chunks, the client then stores the chunks all together into a file. Since this is a response to `OPEN`, the client should already know the name to use for the file.

- `ERR <message>` : Received by the client as a response to `OPEN`. The client should show this error to the user. The client reads nothing else until it sends an `OPEN` message.