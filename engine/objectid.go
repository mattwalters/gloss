package writ

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// newObjectID generates a new 128-bit random object identifier
// formatted as 32 lowercase hexadecimal characters per spec/identifiers.md.
func newObjectID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("writ: crypto/rand read failed: %v", err))
	}
	return hex.EncodeToString(b)
}
