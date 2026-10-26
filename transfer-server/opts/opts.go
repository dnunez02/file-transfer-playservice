// Package opts holds the small helpers the UDP and TCP servers share for reading their
// options and choosing where files come from.
package opts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"example.com/go-file-transfer/transfer-server/filesource"
	"example.com/go-file-transfer/transfer-server/s3"
)

// Source is where a server gets the bytes of a requested file.
type Source interface {
	Fetch(name string) ([]byte, error)
}

// EnvOr returns the environment variable name, or fallback when it is unset or empty.
func EnvOr(name string, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

// ParseSeconds reads the number of seconds out of an option such as "-drain=5".
//
// Inputs: option is a command line argument of the form "-name=N".
// Returns: N seconds as a duration.
// Errors: returns an error naming the option when N is missing, is not an integer, or is
// negative.
// Side effects: none.
func ParseSeconds(option string) (time.Duration, error) {
	value := strings.SplitN(option, "=", 2)[1]
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return 0, fmt.Errorf("invalid %v: expected an integer of 0 or more", option)
	}
	return time.Duration(seconds) * time.Second, nil
}

// ValidPort checks that value is a port number.
//
// Errors: returns an error when value is not an integer from 1 to 65535.
func ValidPort(value string) error {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %q: expected an integer from 1 to 65535", value)
	}
	return nil
}

// OpenSource builds the file source a server was configured with.
//
// Inputs: dir is a local folder (-dir), bucket and prefix describe S3 (-bucket, -prefix).
// Exactly one of dir and bucket must be set.
// Returns: a filesource.Dir for dir, or an S3 source for bucket.
// Errors: returns an error when both or neither of dir and bucket are set, or when the AWS
// configuration cannot be loaded.
// Side effects: loads the AWS configuration when bucket is set.
func OpenSource(dir string, bucket string, prefix string) (Source, error) {
	switch {
	case dir != "" && bucket != "":
		return nil, errors.New("use either -dir=PATH or -bucket=NAME, not both")
	case dir != "":
		return filesource.Dir{Path: dir}, nil
	case bucket != "":
		return s3.NewSource(context.Background(), bucket, prefix)
	}
	return nil, errors.New("missing required option -bucket=NAME (or -dir=PATH for local files)")
}
