package main

import "testing"

func TestFetcherFor(t *testing.T) {
	for _, transport := range []string{"udp", "tcp"} {
		fetch, err := fetcherFor(transport)
		if err != nil || fetch == nil {
			t.Errorf("fetcherFor(%q) = %v, %v", transport, fetch != nil, err)
		}
	}
	if _, err := fetcherFor("quic"); err == nil {
		t.Error("fetcherFor(quic) succeeded, want an error")
	}
}
