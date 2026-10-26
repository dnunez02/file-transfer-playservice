package opts

import (
	"testing"
	"time"
)

func TestParseSeconds(t *testing.T) {
	if got, err := ParseSeconds("-drain=7"); err != nil || got != 7*time.Second {
		t.Errorf("ParseSeconds(-drain=7) = %v, %v", got, err)
	}
	for _, option := range []string{"-drain=", "-drain=x", "-drain=-1"} {
		if _, err := ParseSeconds(option); err == nil {
			t.Errorf("ParseSeconds(%q) succeeded, want an error", option)
		}
	}
}

func TestValidPort(t *testing.T) {
	for value, ok := range map[string]bool{"8080": true, "1": true, "65535": true, "0": false, "65536": false, "x": false, "": false} {
		if err := ValidPort(value); (err == nil) != ok {
			t.Errorf("ValidPort(%q) = %v, want ok=%v", value, err, ok)
		}
	}
}

func TestOpenSourceNeedsExactlyOne(t *testing.T) {
	if _, err := OpenSource("", "", ""); err == nil {
		t.Error("no source given: want an error")
	}
	if _, err := OpenSource("/tmp", "bucket", ""); err == nil {
		t.Error("both given: want an error")
	}
	if source, err := OpenSource("/tmp", "", ""); err != nil || source == nil {
		t.Errorf("OpenSource(dir) = %v, %v", source, err)
	}
}
