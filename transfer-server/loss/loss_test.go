package loss

import "testing"

func TestShouldDropExtremes(t *testing.T) {
	for range 100 {
		if ShouldDrop(0) {
			t.Fatal("0 percent dropped a message")
		}
		if !ShouldDrop(100) {
			t.Fatal("100 percent kept a message")
		}
	}
}

func TestParsePercent(t *testing.T) {
	for option, want := range map[string]int{"-send-loss=0": 0, "-send-loss=30": 30, "-recv-loss=100": 100} {
		if got, err := ParsePercent(option); err != nil || got != want {
			t.Errorf("ParsePercent(%q) = %v, %v; want %v", option, got, err, want)
		}
	}
	for _, option := range []string{"-send-loss=", "-send-loss=x", "-send-loss=-1", "-send-loss=101"} {
		if _, err := ParsePercent(option); err == nil {
			t.Errorf("ParsePercent(%q) succeeded, want an error", option)
		}
	}
}
