package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/VictorMilhomem/freecompute/pkg/freecompute"
)

func TestParseFlagsDefaultsAndOverrides(t *testing.T) {
	defaultImg := "docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e"

	t.Run("defaults with env image", func(t *testing.T) {
		opts, err := parseFlags([]string{
			"--id=alice",
			"--root=/tmp/alice/root",
			"--key=/tmp/keys/alice",
			"--input=/tmp/data.txt",
		}, defaultImg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.id != "alice" || opts.chain != "#vc" || opts.image != defaultImg {
			t.Fatalf("unexpected basic flags: %+v", opts)
		}
		if opts.replicas != 3 || opts.quorum != 2 || opts.reward != 1000 {
			t.Fatalf("unexpected replica/quorum/reward defaults: %+v", opts)
		}
		if opts.commitWindow != 20 || opts.revealWindow != 20 || opts.timeout != 3*time.Minute {
			t.Fatalf("unexpected window/timeout defaults: %+v", opts)
		}
		if opts.allowOwnJobs {
			t.Fatalf("expected allowOwnJobs=false by default")
		}
	})

	t.Run("explicit overrides", func(t *testing.T) {
		overrideImg := "docker.io/library/alpine@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		opts, err := parseFlags([]string{
			"--id=alice",
			"--root=/tmp/alice/root",
			"--key=/tmp/keys/alice",
			"--chain=#custom",
			"--peers=localhost:8332, localhost:8333",
			"--state-dir=/tmp/alice-state",
			"--image=" + overrideImg,
			"--input=/tmp/data.txt",
			"--out=/tmp/alice-out",
			"--replicas=5",
			"--quorum=3",
			"--reward=2500",
			"--commit-window=30",
			"--reveal-window=25",
			"--allow-own-jobs",
			"--timeout=90s",
		}, defaultImg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.chain != "#custom" || opts.stateDir != "/tmp/alice-state" || opts.out != "/tmp/alice-out" || opts.image != overrideImg {
			t.Fatalf("unexpected overrides: %+v", opts)
		}
		if !reflect.DeepEqual(opts.peers, []string{"localhost:8332", "localhost:8333"}) {
			t.Fatalf("unexpected peers: %v", opts.peers)
		}
		if opts.replicas != 5 || opts.quorum != 3 || opts.reward != 2500 || opts.commitWindow != 30 || opts.revealWindow != 25 {
			t.Fatalf("unexpected numeric overrides: %+v", opts)
		}
		if !opts.allowOwnJobs || opts.timeout != 90*time.Second {
			t.Fatalf("unexpected boolean/duration overrides: %+v", opts)
		}
	})

	t.Run("rejects missing or invalid flags", func(t *testing.T) {
		badCases := [][]string{
			{"--unknown-flag"},
			{"--root=/tmp/r", "--key=/tmp/k", "--input=/tmp/in"},
			{"--id=alice", "--key=/tmp/k", "--input=/tmp/in"},
			{"--id=alice", "--root=/tmp/r", "--input=/tmp/in"},
			{"--id=alice", "--root=/tmp/r", "--key=/tmp/k"},
			{"--id=alice", "--root=/tmp/r", "--key=/tmp/k", "--input=/tmp/in", "--replicas=2", "--quorum=3"},
			{"--id=alice", "--root=/tmp/r", "--key=/tmp/k", "--input=/tmp/in", "--commit-window=0"},
			{"--id=alice", "--root=/tmp/r", "--key=/tmp/k", "--input=/tmp/in", "--timeout=0s"},
		}
		for i, args := range badCases {
			if _, err := parseFlags(args, defaultImg); err == nil {
				t.Errorf("case %d (%v): expected error, got nil", i, args)
			}
		}
		if _, err := parseFlags([]string{"--id=alice", "--root=/tmp/r", "--key=/tmp/k", "--input=/tmp/in"}, ""); err == nil {
			t.Errorf("expected error when both --image and VC_IMAGE are empty")
		}
	})
}

func TestFormatReport(t *testing.T) {
	res := freecompute.Result{
		JobID:        "job-123",
		AcceptedHash: "deadbeef",
		OutputCID:    "QmOut",
		Winners:      []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAlice", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBob"},
		Divergent:    []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICarol"},
		NoReveal:     nil,
	}
	got := formatReport(res, "  11\n")
	want := "" +
		"JOB_ID=job-123\n" +
		"ACCEPTED_HASH=deadbeef\n" +
		"VERIFIED=true\n" +
		"RESULT=11\n" +
		"WINNERS=ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAlice,ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBob\n" +
		"DIVERGENT=ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICarol\n" +
		"NO_REVEAL=\n"
	if got != want {
		t.Fatalf("formatReport mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
