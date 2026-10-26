package transferdata

import (
	"fmt"
	"os"
)

type TCP_Pack struct {
	Size int64
	Data [CHUNK_SIZE]byte
}

const CHUNK_SIZE = 64

func ChunkFile(path string) ([]TCP_Pack, error) {

	fmt.Fprintf(os.Stderr, "%v -> \"%v\"", len(path), path)
	file_data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return ChunkBytes(file_data), nil
}

// ChunkBytes splits data into CHUNK_SIZE-byte packs; the last pack may be shorter.
func ChunkBytes(file_data []byte) []TCP_Pack {
	n_chunks := len(file_data) / CHUNK_SIZE
	if len(file_data)%CHUNK_SIZE > 0 {
		n_chunks++
	}
	result := make([]TCP_Pack, n_chunks)
	for chunk_idx := 0; chunk_idx < n_chunks; chunk_idx++ {
		start := chunk_idx * CHUNK_SIZE
		end := min(start+CHUNK_SIZE, len(file_data))
		result[chunk_idx].Size = int64(end - start)
		copy(result[chunk_idx].Data[:], file_data[start:end])
	}

	return result
}

func RecombineData(packs []TCP_Pack) []byte {
	file_size := calculateFileSize(packs)
	result := make([]byte, file_size)

	for pack_idx, elem := range packs {
		for byte_idx := 0; byte_idx < int(elem.Size); byte_idx++ {
			data := elem.Data[byte_idx]
			result[pack_idx*CHUNK_SIZE+byte_idx] = data
		}
	}
	return result
}

func calculateFileSize(packs []TCP_Pack) int {
	sum := 0
	for _, elem := range packs {
		sum += int(elem.Size)
	}
	return sum
}
