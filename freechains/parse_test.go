package freechains

import (
	"os"
	"strings"
	"testing"
)

func TestParseOrder(t *testing.T) {
	t.Run("normal order", func(t *testing.T) {
		f, err := os.Open("testdata/order.txt")
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer f.Close()

		entries, err := ParseOrder(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(entries))
		}
		if entries[0].ID != "3519b231c31e34e6afcb25367a87e232792693e6" || entries[0].Revoked {
			t.Errorf("unexpected entry 0: %+v", entries[0])
		}
		if entries[1].ID != "1553680551e1f90e4d805152f6edf4fd721e009c" || entries[1].Revoked {
			t.Errorf("unexpected entry 1: %+v", entries[1])
		}
		if entries[2].ID != "27c7c85e7cd15328716bbb32c646a5758853a80c" || entries[2].Revoked {
			t.Errorf("unexpected entry 2: %+v", entries[2])
		}
	})

	t.Run("revoked order with blank lines and whitespace", func(t *testing.T) {
		f, err := os.Open("testdata/order_revoked.txt")
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer f.Close()

		entries, err := ParseOrder(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(entries))
		}
		if entries[0].ID != "3519b231c31e34e6afcb25367a87e232792693e6" || !entries[0].Revoked {
			t.Errorf("expected entry 0 revoked, got %+v", entries[0])
		}
		if entries[1].ID != "1553680551e1f90e4d805152f6edf4fd721e009c" || entries[1].Revoked {
			t.Errorf("expected entry 1 unrevoked, got %+v", entries[1])
		}
		if entries[2].ID != "27c7c85e7cd15328716bbb32c646a5758853a80c" || !entries[2].Revoked {
			t.Errorf("expected entry 2 revoked, got %+v", entries[2])
		}
	})
}

func TestParseMetadata(t *testing.T) {
	t.Run("post metadata", func(t *testing.T) {
		f, err := os.Open("testdata/metadata_post.txt")
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer f.Close()

		m, err := ParseMetadata(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if m.ID != "d6568e4000000000000000000000000000000001" {
			t.Errorf("unexpected id: %s", m.ID)
		}
		if m.Kind != ActionPost {
			t.Errorf("expected kind post, got %s", m.Kind)
		}
		if m.Time != 1780088002 {
			t.Errorf("expected time 1780088002, got %d", m.Time)
		}
		if m.Blob != "90c7c77000000000000000000000000000000002" {
			t.Errorf("unexpected blob: %s", m.Blob)
		}
		if m.Signer == nil || *m.Signer != "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKhr2Sz7h+tgvcbcDw1qoLGU0IEsvz+sc0qgA6swK5v5" {
			t.Errorf("unexpected signer: %v", m.Signer)
		}
		if len(m.Backs) != 1 || m.Backs[0] != "b52c62f000000000000000000000000000000003" {
			t.Errorf("unexpected backs: %v", m.Backs)
		}
	})

	t.Run("like metadata", func(t *testing.T) {
		f, err := os.Open("testdata/metadata_like.txt")
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer f.Close()

		m, err := ParseMetadata(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if m.Kind != ActionLike {
			t.Errorf("expected kind like, got %s", m.Kind)
		}
		if m.Amount == nil || *m.Amount != 1000 {
			t.Errorf("expected amount 1000, got %v", m.Amount)
		}
		if m.TargetAuthor == nil || *m.TargetAuthor != "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKhr2Sz7h+tgvcbcDw1qoLGU0IEsvz+sc0qgA6swK5v5" {
			t.Errorf("unexpected target author: %v", m.TargetAuthor)
		}
	})

	t.Run("revoke metadata", func(t *testing.T) {
		f, err := os.Open("testdata/metadata_revoke.txt")
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer f.Close()

		m, err := ParseMetadata(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if m.Kind != ActionRevoke {
			t.Errorf("expected kind revoke, got %s", m.Kind)
		}
		if m.Amount == nil || *m.Amount != -1000 {
			t.Errorf("expected amount -1000, got %v", m.Amount)
		}
		if m.TargetAction == nil || *m.TargetAction != "3519b231c31e34e6afcb25367a87e232792693e6" {
			t.Errorf("unexpected target action: %v", m.TargetAction)
		}
	})

	t.Run("partial metadata (beg post)", func(t *testing.T) {
		f, err := os.Open("testdata/metadata_partial.txt")
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer f.Close()

		m, err := ParseMetadata(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if m.Signer != nil {
			t.Errorf("expected nil signer for beg post, got %v", m.Signer)
		}
		if len(m.Backs) != 0 {
			t.Errorf("expected empty backs, got %v", m.Backs)
		}
	})

	t.Run("metadata with unknown keys ignored", func(t *testing.T) {
		f, err := os.Open("testdata/metadata_unknown.txt")
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer f.Close()

		m, err := ParseMetadata(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if m.ID != "d6568e4000000000000000000000000000000001" {
			t.Errorf("unexpected id: %s", m.ID)
		}
		if m.Kind != ActionPost {
			t.Errorf("expected kind post, got %s", m.Kind)
		}
	})
}

func TestParseInt64(t *testing.T) {
	f, err := os.Open("testdata/reps_single.txt")
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer f.Close()

	n, err := ParseInt64(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 50000 {
		t.Errorf("expected 50000, got %d", n)
	}
}

func TestParseChainID(t *testing.T) {
	t.Run("valid chain id", func(t *testing.T) {
		input := "\n#d279668407b857a619074db2a205d65c55044005\n"
		cid, err := ParseChainID(strings.NewReader(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cid != "#d279668407b857a619074db2a205d65c55044005" {
			t.Errorf("unexpected chain id: %s", cid)
		}
	})

	t.Run("empty chain id fails", func(t *testing.T) {
		_, err := ParseChainID(strings.NewReader("\n  \n"))
		if err == nil {
			t.Fatal("expected error on empty output")
		}
	})
}

func TestParseActionID(t *testing.T) {
	t.Run("valid action id", func(t *testing.T) {
		input := "header line\n3519b231c31e34e6afcb25367a87e232792693e6\n"
		id, err := ParseActionID(strings.NewReader(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "3519b231c31e34e6afcb25367a87e232792693e6" {
			t.Errorf("unexpected action id: %s", id)
		}
	})

	t.Run("empty action id fails", func(t *testing.T) {
		_, err := ParseActionID(strings.NewReader(""))
		if err == nil {
			t.Fatal("expected error on empty output")
		}
	})
}

func TestParseChainsDir(t *testing.T) {
	f, err := os.Open("testdata/chains_dir.txt")
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer f.Close()

	aliases, err := ParseChainsDir(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(aliases) != 3 {
		t.Fatalf("expected 3 aliases, got %d", len(aliases))
	}
	if aliases[0] != "#chat" || aliases[1] != "#compute" || aliases[2] != "#test" {
		t.Errorf("unexpected aliases: %v", aliases)
	}
}

func TestParseRepsTables(t *testing.T) {
	t.Run("reps actions", func(t *testing.T) {
		f, err := os.Open("testdata/reps_actions.txt")
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer f.Close()

		reps, err := ParseRepsActions(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(reps) != 3 {
			t.Fatalf("expected 3 reps entries, got %d", len(reps))
		}
		if reps[0].ID != "3519b231c31e34e6afcb25367a87e232792693e6" || reps[0].Reps != 100 {
			t.Errorf("unexpected entry 0: %+v", reps[0])
		}
	})

	t.Run("reps authors", func(t *testing.T) {
		f, err := os.Open("testdata/reps_authors.txt")
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer f.Close()

		reps, err := ParseRepsAuthors(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(reps) != 2 {
			t.Fatalf("expected 2 author reps, got %d", len(reps))
		}
		if reps[0].Reps != 50000 || !strings.HasPrefix(string(reps[0].Author), "ssh-ed25519") {
			t.Errorf("unexpected entry 0: %+v", reps[0])
		}
	})

	t.Run("reps revokes", func(t *testing.T) {
		f, err := os.Open("testdata/reps_revokes.txt")
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer f.Close()

		reps, err := ParseRepsRevokes(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(reps) != 2 {
			t.Fatalf("expected 2 revoke entries, got %d", len(reps))
		}
		if reps[0].Author != -1000 || reps[0].Others != 0 {
			t.Errorf("unexpected entry 0: %+v", reps[0])
		}
	})

	t.Run("reps revoke single", func(t *testing.T) {
		f, err := os.Open("testdata/reps_revoke_single.txt")
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer f.Close()

		reps, err := ParseRepsRevoke(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if reps.Author != -1000 || reps.Others != 0 {
			t.Errorf("unexpected reps: %+v", reps)
		}
	})
}
