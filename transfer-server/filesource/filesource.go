// Package filesource supplies the bytes of a requested file from a local folder. The S3
// source in package s3 follows the same rules for names.
package filesource

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidName reports whether name is a plain file name. The client chooses the name, so it
// must not be able to reach other files or keys.
func ValidName(name string) bool {
	return name != "" && !strings.HasPrefix(name, ".") && !strings.Contains(name, "/") && !strings.Contains(name, "..")
}

// Dir serves the files of one folder. It is meant for local development and tests; the
// deployed servers read from S3.
type Dir struct {
	Path string
}

// Fetch reads the file called name from the folder. Safe for concurrent use.
//
// Errors: returns an error for an invalid name, or when the file cannot be read.
func (dir Dir) Fetch(name string) ([]byte, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid file name %q", name)
	}
	return os.ReadFile(filepath.Join(dir.Path, name))
}
