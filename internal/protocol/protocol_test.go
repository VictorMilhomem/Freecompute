package protocol

import (
	"bytes"
	"crypto/sha256"
	"reflect"
	"testing"

	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
)

func testHash(label string) []byte {
	sum := sha256.Sum256([]byte(label))
	return sum[:]
}

func testAnnounce() *vcv1.JobAnnounce {
	return &vcv1.JobAnnounce{
		ImageRef:   "docker.io/library/alpine@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Cmd:        []string{"sh", "-c", "wc -w < /input/data.txt > /output/result.txt"},
		InputCid:   "QmTestInputCid",
		Replicas:   3,
		Quorum:     2,
		RewardReps: 1000,
	}
}

func TestCommitmentValidAndTampered(t *testing.T) {
	jobID := "job-123"
	workerID := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAlice"
	resHash := testHash("42\n")
	nonce := testHash("nonce-1")

	c1 := Commitment(jobID, workerID, resHash, nonce)
	c2 := Commitment(jobID, workerID, resHash, nonce)
	if !bytes.Equal(c1, c2) {
		t.Fatalf("expected deterministic commitment")
	}

	if bytes.Equal(c1, Commitment("other-job", workerID, resHash, nonce)) {
		t.Errorf("tampered jobID produced identical commitment")
	}
	if bytes.Equal(c1, Commitment(jobID, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBob", resHash, nonce)) {
		t.Errorf("tampered workerID produced identical commitment")
	}
	if bytes.Equal(c1, Commitment(jobID, workerID, testHash("999\n"), nonce)) {
		t.Errorf("tampered resultHash produced identical commitment")
	}
	if bytes.Equal(c1, Commitment(jobID, workerID, resHash, testHash("nonce-2"))) {
		t.Errorf("tampered nonce produced identical commitment")
	}
}

func TestFirstNCountedAndLaterIgnored(t *testing.T) {
	ann := testAnnounce()
	jobID, err := JobID(ann)
	if err != nil {
		t.Fatalf("JobID error: %v", err)
	}

	h := testHash("result")
	n := testHash("nonce")

	entries := []Entry{
		{Index: 0, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Announce{Announce: ann}}},
		{Index: 1, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "alice", h, n)}}}},
		{Index: 2, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "bob", h, n)}}}},
		{Index: 3, Author: "carol", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "carol", h, n)}}}},
		{Index: 4, Author: "dave", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "dave", h, n)}}}},
	}

	st := Replay(entries)
	if st.Phase != Revealing {
		t.Fatalf("expected phase %s, got %s", Revealing, st.Phase)
	}
	if len(st.Commits) != 3 {
		t.Fatalf("expected 3 counted commits, got %d", len(st.Commits))
	}
	if st.Commits[0].Author != "alice" || st.Commits[1].Author != "bob" || st.Commits[2].Author != "carol" {
		t.Errorf("unexpected commit authors: %+v", st.Commits)
	}
}

func TestDuplicateCommitIgnored(t *testing.T) {
	ann := testAnnounce()
	jobID, _ := JobID(ann)
	h1 := testHash("result-1")
	h2 := testHash("result-2")
	n := testHash("nonce")

	firstCommit := Commitment(jobID, "alice", h1, n)
	secondCommit := Commitment(jobID, "alice", h2, n)

	entries := []Entry{
		{Index: 0, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Announce{Announce: ann}}},
		{Index: 1, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: firstCommit}}}},
		{Index: 2, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: secondCommit}}}},
		{Index: 3, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "bob", h1, n)}}}},
	}

	st := Replay(entries)
	if st.Phase != Committing {
		t.Fatalf("expected phase %s because only 2 distinct authors committed, got %s", Committing, st.Phase)
	}
	if len(st.Commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(st.Commits))
	}
	if !bytes.Equal(st.Commits[0].Commitment, firstCommit) {
		t.Errorf("expected first commit from alice to win")
	}
}

func TestRevealValidationAndWithoutCommitIgnored(t *testing.T) {
	ann := testAnnounce()
	jobID, _ := JobID(ann)
	h := testHash("good-result")
	badH := testHash("tampered-result")
	n := testHash("nonce")

	entries := []Entry{
		{Index: 0, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Announce{Announce: ann}}},
		{Index: 1, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "alice", h, n)}}}},
		// Early reveal before N commits must be ignored.
		{Index: 2, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmRes", Nonce: n}}}},
		{Index: 3, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "bob", h, n)}}}},
		{Index: 4, Author: "carol", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "carol", h, n)}}}},
		// Reveal from dave (not in counted commits) must be ignored.
		{Index: 5, Author: "dave", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmRes", Nonce: n}}}},
		// Tampered reveal from bob (wrong hash) must be ignored.
		{Index: 6, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: badH, ResultCid: "QmBad", Nonce: n}}}},
		// Valid reveal from alice.
		{Index: 7, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmRes", Nonce: n}}}},
	}

	st := Replay(entries)
	if len(st.Reveals) != 1 || st.Reveals[0].Author != "alice" {
		t.Fatalf("expected only alice's valid reveal after phase open, got %+v", st.Reveals)
	}
}

func TestTallyWithAndWithoutQuorum(t *testing.T) {
	hGood := testHash("good")
	hBad1 := testHash("bad-1")
	hBad2 := testHash("bad-2")

	commits := []Commit{
		{Index: 1, Author: "alice"},
		{Index: 2, Author: "bob"},
		{Index: 3, Author: "carol"},
	}

	t.Run("quorum met with divergent and no_reveal", func(t *testing.T) {
		reveals := []Reveal{
			{Index: 4, Author: "alice", ResultHash: hGood, ResultCID: "QmGood"},
			{Index: 5, Author: "bob", ResultHash: hGood, ResultCID: "QmGood"},
			{Index: 6, Author: "carol", ResultHash: hBad1, ResultCID: "QmBad"},
		}
		v := Tally(commits, reveals, 2)
		if !v.HasQuorum {
			t.Fatalf("expected quorum")
		}
		if !bytes.Equal(v.AcceptedHash, hGood) {
			t.Errorf("unexpected accepted hash")
		}
		if !reflect.DeepEqual(v.Winners, []string{"alice", "bob"}) {
			t.Errorf("unexpected winners: %v", v.Winners)
		}
		if !reflect.DeepEqual(v.Divergent, []string{"carol"}) {
			t.Errorf("unexpected divergent: %v", v.Divergent)
		}
		if len(v.NoReveal) != 0 {
			t.Errorf("expected empty no_reveal, got %v", v.NoReveal)
		}
	})

	t.Run("quorum met with non-revealing worker", func(t *testing.T) {
		reveals := []Reveal{
			{Index: 4, Author: "alice", ResultHash: hGood, ResultCID: "QmGood"},
			{Index: 5, Author: "bob", ResultHash: hGood, ResultCID: "QmGood"},
		}
		v := Tally(commits, reveals, 2)
		if !v.HasQuorum {
			t.Fatalf("expected quorum")
		}
		if !reflect.DeepEqual(v.Winners, []string{"alice", "bob"}) {
			t.Errorf("unexpected winners: %v", v.Winners)
		}
		if !reflect.DeepEqual(v.NoReveal, []string{"carol"}) {
			t.Errorf("unexpected no_reveal: %v", v.NoReveal)
		}
	})

	t.Run("without quorum", func(t *testing.T) {
		reveals := []Reveal{
			{Index: 4, Author: "alice", ResultHash: hGood, ResultCID: "QmGood"},
			{Index: 5, Author: "bob", ResultHash: hBad1, ResultCID: "QmBad1"},
			{Index: 6, Author: "carol", ResultHash: hBad2, ResultCID: "QmBad2"},
		}
		v := Tally(commits, reveals, 2)
		if v.HasQuorum {
			t.Fatalf("expected no quorum when all 3 reveals differ")
		}
		if len(v.Winners) != 0 {
			t.Errorf("expected no winners, got %v", v.Winners)
		}
	})
}

func TestReplayDeterminism(t *testing.T) {
	ann := testAnnounce()
	jobID, _ := JobID(ann)
	h := testHash("result")
	hDiv := testHash("999")
	nA := testHash("nonce-a")
	nB := testHash("nonce-b")
	nC := testHash("nonce-c")

	entries := []Entry{
		{Index: 0, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Announce{Announce: ann}}},
		{Index: 1, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "alice", h, nA)}}}},
		{Index: 2, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "bob", h, nB)}}}},
		{Index: 3, Author: "carol", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "carol", hDiv, nC)}}}},
		{Index: 4, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmGood", Nonce: nA}}}},
		{Index: 5, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmGood", Nonce: nB}}}},
		{Index: 6, Author: "carol", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: hDiv, ResultCid: "QmDiv", Nonce: nC}}}},
		{Index: 7, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Finalize{Finalize: &vcv1.JobFinalize{AcceptedHash: h, Winners: []string{"alice", "bob"}, Divergent: []string{"carol"}}}}},
	}

	s1 := Replay(entries)
	s2 := Replay(entries)
	if !reflect.DeepEqual(s1, s2) {
		t.Fatalf("Replay is not deterministic:\n%+v\nvs\n%+v", s1, s2)
	}
	if s1.Phase != Done {
		t.Fatalf("expected Done phase, got %s", s1.Phase)
	}
}
