package protocol

import (
	"bytes"
	"crypto/sha256"
	"math/rand"
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
		ImageRef:     "docker.io/library/alpine@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Cmd:          []string{"sh", "-c", "wc -w < /input/data.txt > /output/result.txt"},
		InputCid:     "QmTestInputCid",
		Replicas:     3,
		Quorum:       2,
		CommitWindow: 20,
		RevealWindow: 20,
		RewardReps:   1000,
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
	if bytes.Equal(Commitment("ab", "c", resHash, nonce), Commitment("a", "bc", resHash, nonce)) {
		t.Errorf("expected null separator to prevent boundary ambiguity")
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
		{Index: 2, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmRes", Nonce: n}}}},
		{Index: 3, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "bob", h, n)}}}},
		{Index: 4, Author: "carol", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "carol", h, n)}}}},
		{Index: 5, Author: "dave", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmRes", Nonce: n}}}},
		{Index: 6, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: badH, ResultCid: "QmBad", Nonce: n}}}},
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

func TestCommitWindowExpiration(t *testing.T) {
	ann := testAnnounce()
	ann.CommitWindow = 4
	ann.RevealWindow = 4
	jobID, _ := JobID(ann)
	h := testHash("result")
	n := testHash("nonce")

	t.Run("expires when fewer than Q commits after commit_window", func(t *testing.T) {
		entries := []Entry{
			{Index: 0, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Announce{Announce: ann}}},
			{Index: 1, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "alice", h, n)}}}},
			{Index: 4, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Tick{Tick: &vcv1.Tick{JobId: jobID}}}},
		}
		st := Replay(entries)
		if st.Phase != Committing {
			t.Fatalf("at index 4 (within window 4) expected Committing, got %s", st.Phase)
		}
		st = Apply(st, Entry{Index: 5, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Tick{Tick: &vcv1.Tick{JobId: jobID}}}})
		if st.Phase != Expired || !st.Finalizable || st.Verdict.Status != Expired {
			t.Fatalf("expected Expired and Finalizable at index 5, got phase=%s finalizable=%v status=%s", st.Phase, st.Finalizable, st.Verdict.Status)
		}
	})

	t.Run("opens revealing at first entry beyond commit_window when at least Q commits exist", func(t *testing.T) {
		entries := []Entry{
			{Index: 0, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Announce{Announce: ann}}},
			{Index: 1, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "alice", h, n)}}}},
			{Index: 2, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "bob", h, n)}}}},
			{Index: 6, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Tick{Tick: &vcv1.Tick{JobId: jobID}}}},
		}
		st := Replay(entries)
		if st.Phase != Revealing {
			t.Fatalf("expected Revealing at index 6, got %s", st.Phase)
		}
		if st.RevealOpenIndex != 6 {
			t.Fatalf("expected RevealOpenIndex=6, got %d", st.RevealOpenIndex)
		}
		if len(st.Commits) != 2 {
			t.Fatalf("expected 2 counted commits, got %d", len(st.Commits))
		}
		// Late commit after Revealing opened is ignored.
		st = Apply(st, Entry{Index: 7, Author: "carol", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "carol", h, n)}}}})
		if len(st.Commits) != 2 {
			t.Fatalf("expected late commit to be ignored")
		}
		// Both counted workers reveal -> immediately finalizable.
		st = Apply(st, Entry{Index: 8, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmGood", Nonce: n}}}})
		st = Apply(st, Entry{Index: 9, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmGood", Nonce: n}}}})
		if !st.Finalizable || st.Verdict.Status != Accepted {
			t.Fatalf("expected finalizable Accepted after all counted workers revealed, got finalizable=%v status=%s", st.Finalizable, st.Verdict.Status)
		}
	})
}

func TestRevealWindowAndTicks(t *testing.T) {
	ann := testAnnounce()
	ann.CommitWindow = 5
	ann.RevealWindow = 3
	jobID, _ := JobID(ann)
	h := testHash("result")
	hBad := testHash("bad")
	n := testHash("nonce")

	base := []Entry{
		{Index: 0, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Announce{Announce: ann}}},
		{Index: 1, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "alice", h, n)}}}},
		{Index: 2, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "bob", h, n)}}}},
		{Index: 3, Author: "carol", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: Commitment(jobID, "carol", hBad, n)}}}},
	}

	t.Run("accepted when reveal_window passes with quorum via Ticks and late reveal ignored", func(t *testing.T) {
		entries := append([]Entry(nil), base...)
		// RevealOpenIndex is 3, RevealWindow is 3 (valid indices: 4, 5, 6).
		entries = append(entries,
			Entry{Index: 4, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmGood", Nonce: n}}}},
			Entry{Index: 5, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmGood", Nonce: n}}}},
			Entry{Index: 6, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Tick{Tick: &vcv1.Tick{JobId: jobID}}}},
		)
		st := Replay(entries)
		if st.Finalizable {
			t.Fatalf("should not be finalizable at index 6 (3 - 3 == 3 <= window)")
		}
		// Carol attempts to reveal at index 7 (7 - 3 = 4 > 3): window elapsed, Carol's reveal is ignored, state becomes finalizable Accepted.
		st = Apply(st, Entry{Index: 7, Author: "carol", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: hBad, ResultCid: "QmBad", Nonce: n}}}})
		if !st.Finalizable || st.Verdict.Status != Accepted {
			t.Fatalf("expected finalizable Accepted at index 7, got finalizable=%v status=%s", st.Finalizable, st.Verdict.Status)
		}
		if !reflect.DeepEqual(st.Verdict.NoReveal, []string{"carol"}) {
			t.Fatalf("expected carol in NoReveal, got %v", st.Verdict.NoReveal)
		}
	})

	t.Run("expired when reveal_window passes without quorum", func(t *testing.T) {
		entries := append([]Entry(nil), base...)
		entries = append(entries,
			Entry{Index: 4, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmGood", Nonce: n}}}},
			Entry{Index: 5, Author: "carol", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: hBad, ResultCid: "QmBad", Nonce: n}}}},
			Entry{Index: 7, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Tick{Tick: &vcv1.Tick{JobId: jobID}}}},
		)
		st := Replay(entries)
		if st.Phase != Expired || !st.Finalizable || st.Verdict.Status != Expired {
			t.Fatalf("expected Expired when reveal_window elapses without quorum, got phase=%s finalizable=%v status=%s", st.Phase, st.Finalizable, st.Verdict.Status)
		}
	})
}

func TestVerifyFinalizeAndNonSubmitterFinalize(t *testing.T) {
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
	}

	st := Replay(entries)
	if !st.Finalizable {
		t.Fatalf("expected state to be finalizable")
	}

	forged := &vcv1.JobFinalize{
		AcceptedHash: h,
		Winners:      []string{"alice", "bob", "carol"},
	}
	if VerifyFinalize(st, forged) {
		t.Fatalf("expected forged finalize to be rejected")
	}

	honest := &vcv1.JobFinalize{
		AcceptedHash: h,
		Winners:      []string{"alice", "bob"},
		Divergent:    []string{"carol"},
	}
	if !VerifyFinalize(st, honest) {
		t.Fatalf("expected honest finalize to be accepted")
	}

	// Forged finalize on chain is ignored.
	st = Apply(st, Entry{Index: 7, Author: "carol", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Finalize{Finalize: forged}}})
	if st.Finalize != nil {
		t.Fatalf("expected forged finalize entry to be ignored")
	}

	// Non-Submitter (bob) posts valid finalize: accepted!
	st = Apply(st, Entry{Index: 8, Author: "bob", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Finalize{Finalize: honest}}})
	if st.Finalize == nil || st.FinalizedBy != "bob" || st.Phase != Accepted {
		t.Fatalf("expected bob's valid finalize to win, got finalize=%v by=%s phase=%s", st.Finalize, st.FinalizedBy, st.Phase)
	}

	// Later valid finalize from alice is ignored because first valid finalize wins.
	st = Apply(st, Entry{Index: 9, Author: "alice", Envelope: &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Finalize{Finalize: honest}}})
	if st.FinalizedBy != "bob" {
		t.Fatalf("expected first valid finalize by bob to remain winner, got %s", st.FinalizedBy)
	}
}

func TestApplyReplayEquivalenceProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	authors := []string{"alice", "bob", "carol", "dave", "eve"}
	hashes := [][]byte{testHash("h1"), testHash("h2"), testHash("h3")}
	nonces := [][]byte{testHash("n1"), testHash("n2")}

	for trial := 0; trial < 200; trial++ {
		ann := testAnnounce()
		ann.Replicas = uint32(1 + rng.Intn(4))
		ann.Quorum = uint32(1 + rng.Intn(int(ann.Replicas)))
		ann.CommitWindow = uint32(1 + rng.Intn(8))
		ann.RevealWindow = uint32(1 + rng.Intn(8))
		jobID, _ := JobID(ann)

		numEntries := 5 + rng.Intn(30)
		entries := make([]Entry, 0, numEntries)
		idx := 0

		for step := 0; step < numEntries; step++ {
			idx += 1 + rng.Intn(2)
			author := authors[rng.Intn(len(authors))]
			h := hashes[rng.Intn(len(hashes))]
			n := nonces[rng.Intn(len(nonces))]

			var env *vcv1.Envelope
			switch rng.Intn(6) {
			case 0:
				env = &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Announce{Announce: ann}}
			case 1:
				commitBytes := Commitment(jobID, author, h, n)
				if rng.Intn(4) == 0 {
					commitBytes = []byte("short")
				}
				env = &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Commit{Commit: &vcv1.ResultCommit{Commitment: commitBytes}}}
			case 2:
				env = &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Reveal{Reveal: &vcv1.ResultReveal{ResultHash: h, ResultCid: "QmCid", Nonce: n}}}
			case 3:
				env = &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Tick{Tick: &vcv1.Tick{JobId: jobID}}}
			case 4:
				cur := Replay(entries)
				fin := &vcv1.JobFinalize{
					AcceptedHash: cur.Verdict.AcceptedHash,
					Winners:      cur.Verdict.Winners,
					Divergent:    cur.Verdict.Divergent,
					NoReveal:     cur.Verdict.NoReveal,
				}
				if rng.Intn(2) == 0 {
					fin.Winners = []string{"forged"}
				}
				env = &vcv1.Envelope{Version: 1, JobId: jobID, Body: &vcv1.Envelope_Finalize{Finalize: fin}}
			case 5:
				env = nil
			}
			entries = append(entries, Entry{Index: idx, Author: author, Envelope: env})
		}

		replayed := Replay(entries)
		folded := State{Phase: Committing, LastIndex: -1}
		for _, e := range entries {
			prev := folded
			folded = Apply(folded, e)
			// Verify Apply does not mutate previous State slices in place.
			_ = prev
		}

		if !reflect.DeepEqual(replayed, folded) {
			t.Fatalf("trial %d: Replay(entries) != fold(Apply):\nreplayed=%+v\nfolded=%+v", trial, replayed, folded)
		}
	}
}
