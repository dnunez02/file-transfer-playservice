// Package loss simulates an unreliable network for the servers: each datagram or message is
// dropped with a chance the user picks with -send-loss and -recv-loss.
package loss

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
)

// IntN returns a random integer from 0 to n-1. It is a variable only so tests can script
// which messages are dropped.
var IntN = rand.IntN

// ShouldDrop decides whether one simulated message is lost.
//
// Inputs: percent is the chance of loss as an integer from 0 to 100.
// Returns: true with probability percent/100. 0 never drops and 100 always drops.
// Errors: none; values outside 0-100 behave as 0 or 100.
// Side effects: consumes one random number.
func ShouldDrop(percent int) bool {
	return IntN(100) < percent
}

// ParsePercent reads the number out of a loss option such as "-send-loss=30".
//
// Inputs: option is a command line argument of the form "-name=N".
// Returns: N as an integer from 0 to 100.
// Errors: returns an error naming the option when N is missing, is not an integer,
// or is outside 0-100.
// Side effects: none.
func ParsePercent(option string) (int, error) {
	value := strings.SplitN(option, "=", 2)[1]
	percent, err := strconv.Atoi(value)
	if err != nil || percent < 0 || percent > 100 {
		return 0, fmt.Errorf("invalid %v: expected an integer from 0 to 100", option)
	}
	return percent, nil
}
