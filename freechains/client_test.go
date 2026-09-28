package freechains

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type fakeRunner struct {
	mu       sync.Mutex
	recorded []Command
	script   func(cmd Command) (Result, error)
}

func (f *fakeRunner) Run(ctx context.Context, cmd Command) (Result, error) {
	f.mu.Lock()
	f.recorded = append(f.recorded, cmd)
	f.mu.Unlock()

	if f.script != nil {
		return f.script(cmd)
	}
	return Result{Stdout: []byte(""), ExitCode: 0}, nil
}

func (f *fakeRunner) lastCommand() Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.recorded) == 0 {
		return Command{}
	}
	return f.recorded[len(f.recorded)-1]
}

func TestClientValidation(t *testing.T) {
	runner := &fakeRunner{}
	ctx := context.Background()

	t.Run("bad alias validation", func(t *testing.T) {
		client, err := NewClient(runner, Config{})
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		badAliases := []Alias{
			"",
			"chat",
			"#",
			"#chat room",
			"#chat\troom",
		}

		for _, a := range badAliases {
			if _, err := client.ListOrder(ctx, a); !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("expected ErrInvalidArgument for alias %q, got %v", a, err)
			}
		}
	})

	t.Run("post options validation", func(t *testing.T) {
		client, _ := NewClient(runner, Config{})

		// Neither sign nor beg
		_, err := client.Post(ctx, "#chat", InlinePayload("hi"), PostOptions{})
		if !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument when neither sign nor beg is set, got %v", err)
		}

		// Both sign and beg
		_, err = client.Post(ctx, "#chat", InlinePayload("hi"), PostOptions{
			Sign: "/path/to/key",
			Beg:  true,
		})
		if !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument when both sign and beg are set, got %v", err)
		}
	})

	t.Run("default key applied", func(t *testing.T) {
		defaultKey := PrivateKeyPath("/home/user/.ssh/default_id")
		client, _ := NewClient(runner, Config{
			DefaultKey: defaultKey,
		})

		runner.script = func(cmd Command) (Result, error) {
			return Result{
				Stdout:   []byte("3519b231c31e34e6afcb25367a87e232792693e6\n"),
				ExitCode: 0,
			}, nil
		}

		// No sign and no beg passed -> should use DefaultKey
		id, err := client.Post(ctx, "#chat", InlinePayload("hi"), PostOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "3519b231c31e34e6afcb25367a87e232792693e6" {
			t.Errorf("unexpected id: %s", id)
		}

		lastArgs := runner.lastCommand().Args
		expectedSignArg := "--sign=" + string(defaultKey)
		found := false
		for _, arg := range lastArgs {
			if arg == expectedSignArg {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s in args: %v", expectedSignArg, lastArgs)
		}

		// If Beg is true, default key should NOT be applied (no collision)
		_, err = client.Post(ctx, "#chat", InlinePayload("hi"), PostOptions{Beg: true})
		if err != nil {
			t.Fatalf("unexpected error when posting with Beg=true: %v", err)
		}
		lastArgs = runner.lastCommand().Args
		for _, arg := range lastArgs {
			if strings.HasPrefix(arg, "--sign=") {
				t.Fatalf("unexpected --sign in beg post: %v", lastArgs)
			}
		}
	})

	t.Run("oversized payload validation", func(t *testing.T) {
		client, _ := NewClient(runner, Config{})

		// 128 KB + 1 byte
		oversized := strings.Repeat("x", 128*1024+1)
		_, err := client.Post(ctx, "#chat", InlinePayload(oversized), PostOptions{Beg: true})
		if !errors.Is(err, ErrPayloadTooLarge) {
			t.Fatalf("expected ErrPayloadTooLarge, got %v", err)
		}

		// Valid 128 KB
		exact := strings.Repeat("x", 128*1024)
		runner.script = func(cmd Command) (Result, error) {
			return Result{Stdout: []byte("3519b231c31e34e6afcb25367a87e232792693e6\n")}, nil
		}
		_, err = client.Post(ctx, "#chat", InlinePayload(exact), PostOptions{Beg: true})
		if err != nil {
			t.Fatalf("expected exact 128 KB payload to pass validation, got %v", err)
		}
	})

	t.Run("negative vote amount validation", func(t *testing.T) {
		client, _ := NewClient(runner, Config{})
		actID := ActionID("3519b231c31e34e6afcb25367a87e232792693e6")

		if _, err := client.Like(ctx, "#chat", -10, ActionTarget(actID), VoteOptions{}); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for negative like amount, got %v", err)
		}
		if _, err := client.Dislike(ctx, "#chat", -5, ActionTarget(actID), VoteOptions{}); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for negative dislike amount, got %v", err)
		}
		if _, err := client.Revoke(ctx, "#chat", -1, actID, VoteOptions{}); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for negative revoke amount, got %v", err)
		}
		if _, err := client.Unrevoke(ctx, "#chat", -1, actID, VoteOptions{}); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for negative unrevoke amount, got %v", err)
		}
	})

	t.Run("option injection rejection", func(t *testing.T) {
		client, _ := NewClient(runner, Config{})
		actID := ActionID("3519b231c31e34e6afcb25367a87e232792693e6")

		// Leading '-' on inline payload text
		if _, err := client.Post(ctx, "#chat", InlinePayload("--option-injection"), PostOptions{Beg: true}); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for inline text starting with '-', got %v", err)
		}

		// Leading '-' on action id
		if _, err := client.GetMetadata(ctx, "#chat", ActionID("-bad-id")); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for action id starting with '-', got %v", err)
		}

		// Leading '-' on public key
		if _, err := client.RepsAuthor(ctx, "#chat", PublicKey("-pvt-key")); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for public key starting with '-', got %v", err)
		}

		// Leading '-' on private key path
		if _, err := client.Post(ctx, "#chat", InlinePayload("ok"), PostOptions{Sign: PrivateKeyPath("-bad-path")}); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for key path starting with '-', got %v", err)
		}

		// Leading '-' on clone url
		if _, err := client.ChainsAddClone(ctx, "#chat", "--host=evil"); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for clone url starting with '-', got %v", err)
		}

		// Leading '-' on sync remote
		if err := client.SyncRecv(ctx, "#chat", "--host=evil"); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for sync remote starting with '-', got %v", err)
		}

		// Leading '-' on vote file path
		if _, err := client.Like(ctx, "#chat", 100, ActionTarget(actID), VoteOptions{File: "--file=bad"}); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for vote file starting with '-', got %v", err)
		}
	})
}

func TestClientMethods(t *testing.T) {
	ctx := context.Background()
	actID := ActionID("3519b231c31e34e6afcb25367a87e232792693e6")
	alias := Alias("#chat")

	t.Run("chains methods", func(t *testing.T) {
		runner := &fakeRunner{}
		client, _ := NewClient(runner, Config{})

		runner.script = func(cmd Command) (Result, error) {
			return Result{Stdout: []byte("#chat\n#compute\n")}, nil
		}
		aliases, err := client.ChainsDir(ctx)
		if err != nil || len(aliases) != 2 {
			t.Fatalf("unexpected ChainsDir result: %v, %v", aliases, err)
		}

		runner.script = func(cmd Command) (Result, error) {
			return Result{Stdout: []byte("#d279668407b857a619074db2a205d65c55044005\n")}, nil
		}
		cid, err := client.ChainsAddInit(ctx, alias)
		if err != nil || cid != "#d279668407b857a619074db2a205d65c55044005" {
			t.Fatalf("unexpected ChainsAddInit result: %s, %v", cid, err)
		}

		cid, err = client.ChainsAddClone(ctx, alias, "localhost:8330")
		if err != nil || cid != "#d279668407b857a619074db2a205d65c55044005" {
			t.Fatalf("unexpected ChainsAddClone result: %s, %v", cid, err)
		}

		runner.script = func(cmd Command) (Result, error) {
			return Result{Stdout: []byte("")}, nil
		}
		if err := client.ChainsRem(ctx, alias); err != nil {
			t.Fatalf("unexpected ChainsRem error: %v", err)
		}
	})

	t.Run("post, votes, queries", func(t *testing.T) {
		runner := &fakeRunner{}
		client, _ := NewClient(runner, Config{})

		runner.script = func(cmd Command) (Result, error) {
			return Result{Stdout: []byte(string(actID) + "\n")}, nil
		}

		id, err := client.Like(ctx, alias, 100, ActionTarget(actID), VoteOptions{Sign: "/tmp/key"})
		if err != nil || id != actID {
			t.Fatalf("unexpected Like: %v, %v", id, err)
		}

		id, err = client.Dislike(ctx, alias, 100, ActionTarget(actID), VoteOptions{Sign: "/tmp/key"})
		if err != nil || id != actID {
			t.Fatalf("unexpected Dislike: %v, %v", id, err)
		}

		id, err = client.Revoke(ctx, alias, 100, actID, VoteOptions{Sign: "/tmp/key"})
		if err != nil || id != actID {
			t.Fatalf("unexpected Revoke: %v, %v", id, err)
		}

		id, err = client.Unrevoke(ctx, alias, 100, actID, VoteOptions{Sign: "/tmp/key"})
		if err != nil || id != actID {
			t.Fatalf("unexpected Unrevoke: %v, %v", id, err)
		}

		// ListDag
		runner.script = func(cmd Command) (Result, error) {
			return Result{Stdout: []byte("* dag representation\n")}, nil
		}
		dag, err := client.ListDag(ctx, alias)
		if err != nil || dag != "* dag representation\n" {
			t.Fatalf("unexpected ListDag: %q, %v", dag, err)
		}

		// ListBegs
		runner.script = func(cmd Command) (Result, error) {
			return Result{Stdout: []byte(string(actID) + "\n")}, nil
		}
		begs, err := client.ListBegs(ctx, alias)
		if err != nil || len(begs) != 1 || begs[0] != actID {
			t.Fatalf("unexpected ListBegs: %v, %v", begs, err)
		}

		// ListRevokes
		revokes, err := client.ListRevokes(ctx, alias)
		if err != nil || len(revokes) != 1 || revokes[0] != actID {
			t.Fatalf("unexpected ListRevokes: %v, %v", revokes, err)
		}

		// GetPayload
		runner.script = func(cmd Command) (Result, error) {
			return Result{Stdout: []byte("payload content")}, nil
		}
		payloadBytes, err := client.GetPayload(ctx, alias, actID)
		if err != nil || string(payloadBytes) != "payload content" {
			t.Fatalf("unexpected GetPayload: %s, %v", string(payloadBytes), err)
		}

		// Sync
		if err := client.SyncRecv(ctx, alias, "localhost:8330"); err != nil {
			t.Fatalf("unexpected SyncRecv error: %v", err)
		}
		if err := client.SyncSend(ctx, alias, "localhost:8330"); err != nil {
			t.Fatalf("unexpected SyncSend error: %v", err)
		}

		// Abandon and Sweep
		if err := client.Abandon(ctx, alias, actID, true); err != nil {
			t.Fatalf("unexpected Abandon error: %v", err)
		}
		if err := client.Sweep(ctx, alias); err != nil {
			t.Fatalf("unexpected Sweep error: %v", err)
		}
	})

	t.Run("error classification and command error fallback", func(t *testing.T) {
		runner := &fakeRunner{}
		client, _ := NewClient(runner, Config{})

		// Classified error
		runner.script = func(cmd Command) (Result, error) {
			return Result{
				Stderr:   []byte("ERROR : chain post : insufficient reputation\n"),
				ExitCode: 1,
			}, nil
		}
		_, err := client.Post(ctx, alias, InlinePayload("hi"), PostOptions{Beg: true})
		if !errors.Is(err, ErrInsufficientReps) {
			t.Fatalf("expected ErrInsufficientReps, got %v", err)
		}

		// Unclassified error -> CommandError
		runner.script = func(cmd Command) (Result, error) {
			return Result{
				Stderr:   []byte("completely unknown crash\n"),
				ExitCode: 2,
			}, nil
		}
		_, err = client.Post(ctx, alias, InlinePayload("hi"), PostOptions{Beg: true})
		var cmdErr *CommandError
		if !errors.As(err, &cmdErr) {
			t.Fatalf("expected *CommandError for unclassified failure, got %v", err)
		}
		if cmdErr.ExitCode != 2 {
			t.Errorf("expected exit code 2, got %d", cmdErr.ExitCode)
		}
		if !reflect.DeepEqual(cmdErr.Args, []string{"freechains", "chain", "#chat", "post", "inline", "hi", "--beg"}) {
			t.Errorf("unexpected command args: %v", cmdErr.Args)
		}
	})
}
