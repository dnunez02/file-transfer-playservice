package transferdata

import (
	"bytes"
	"testing"
)

func TestChunkBytes(t *testing.T) {
	for _, size := range []int{0, 1, 64, 65, 128, 200} {
		data := bytes.Repeat([]byte{7}, size)
		packs := ChunkBytes(data)
		if got := RecombineData(packs); !bytes.Equal(got, data) {
			t.Errorf("size %d: round trip returned %d bytes", size, len(got))
		}
		if want := (size + CHUNK_SIZE - 1) / CHUNK_SIZE; len(packs) != want {
			t.Errorf("size %d: %d packs, want %d", size, len(packs), want)
		}
	}
}
