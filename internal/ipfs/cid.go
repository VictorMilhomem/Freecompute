package ipfs

import (
	"fmt"

	gocid "github.com/ipfs/go-cid"
)

// CID represents a validated, content-addressed identifier.
type CID string

// ParseCID validates and converts a string into a CID.
func ParseCID(s string) (CID, error) {
	if s == "" {
		return "", fmt.Errorf("%w: empty CID string", ErrInvalidCID)
	}
	c, err := gocid.Decode(s)
	if err != nil || !c.Defined() {
		return "", fmt.Errorf("%w: %v", ErrInvalidCID, err)
	}
	return CID(c.String()), nil
}

// String returns the CID encoded as a string.
func (c CID) String() string {
	return string(c)
}

// Defined reports whether the CID is non-empty.
func (c CID) Defined() bool {
	return c != ""
}
