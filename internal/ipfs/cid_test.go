package ipfs

import (
	"errors"
	"testing"
)

func TestCID(t *testing.T) {
	t.Run("valid v1 CID", func(t *testing.T) {
		validCID := "bafybeicg2abbgahf5oz3whswsqx5qjfs4xedr4eamrf7eefk672ea63p3y"
		c, err := ParseCID(validCID)
		if err != nil {
			t.Fatalf("unexpected error parsing valid CID: %v", err)
		}
		if c.String() != validCID {
			t.Errorf("got %s, want %s", c.String(), validCID)
		}
		if !c.Defined() {
			t.Error("expected Defined() to be true")
		}
	})

	t.Run("valid v0 CID", func(t *testing.T) {
		validV0 := "QmXoypizjW3WknFiJnKLwHCnL72vedxjQkDDP1mXWo6uco"
		c, err := ParseCID(validV0)
		if err != nil {
			t.Fatalf("unexpected error parsing v0 CID: %v", err)
		}
		if c.String() != validV0 {
			t.Errorf("got %s, want %s", c.String(), validV0)
		}
	})

	t.Run("invalid CID", func(t *testing.T) {
		invalids := []string{
			"",
			"not-a-cid",
			"Qm123",
			"bafybeig-invalid!!",
		}
		for _, s := range invalids {
			_, err := ParseCID(s)
			if err == nil {
				t.Errorf("expected error for invalid CID %q, got nil", s)
			}
			if !errors.Is(err, ErrInvalidCID) {
				t.Errorf("expected ErrInvalidCID for %q, got %v", s, err)
			}
		}
	})
}
