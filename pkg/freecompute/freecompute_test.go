package freecompute

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/VictorMilhomem/freecompute/internal/actors"
	"github.com/VictorMilhomem/freecompute/internal/canon"
	"github.com/VictorMilhomem/freecompute/internal/protocol"
	"github.com/VictorMilhomem/freecompute/internal/sandbox"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
	"google.golang.org/protobuf/proto"
)

const testAlpineImage = "docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e"

type fakeChainHub struct {
	mu      sync.Mutex
	seq     int
	order   []OrderItem
	meta    map[string]ActionMeta
	payload map[string][]byte
	reps    map[string]int64
	votes   []recordedVote
	block   map[string]bool
}

type recordedVote struct {
	voter  string
	like   bool
	amount int64
	target string
}

func newFakeChainHub() *fakeChainHub {
	return &fakeChainHub{
		meta:    make(map[string]ActionMeta),
		payload: make(map[string][]byte),
		reps: map[string]int64{
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAlice": 20000,
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBob":   20000,
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICarol": 20000,
		},
		block: make(map[string]bool),
	}
}

func (h *fakeChainHub) clientFor(pubKey string) *fakeChainClient {
	return &fakeChainClient{hub: h, pubKey: pubKey}
}

func (h *fakeChainHub) postAs(pubKey string, payload []byte) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.block[pubKey] {
		return "", fmt.Errorf("post blocked for %s", pubKey)
	}
	h.seq++
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d-%s", h.seq, pubKey)))
	id := hex.EncodeToString(sum[:20])

	h.order = append(h.order, OrderItem{ID: id})
	h.meta[id] = ActionMeta{
		ID:     id,
		Kind:   "post",
		Signer: pubKey,
		Time:   int64(h.seq),
	}
	h.payload[id] = append([]byte(nil), payload...)
	h.reps[pubKey] -= DefaultPostCost
	return id, nil
}

func (h *fakeChainHub) setBlocked(pubKey string, blocked bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.block[pubKey] = blocked
}

func (h *fakeChainHub) countCommitsBy(author string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := 0
	for _, item := range h.order {
		m := h.meta[item.ID]
		if m.Kind != "post" || m.Signer != author {
			continue
		}
		env := &vcv1.Envelope{}
		if err := proto.Unmarshal(h.payload[item.ID], env); err == nil && env.GetCommit() != nil {
			count++
		}
	}
	return count
}

func (h *fakeChainHub) getVotes() []recordedVote {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]recordedVote(nil), h.votes...)
}

type fakeChainClient struct {
	hub         *fakeChainHub
	pubKey      string
	onPost      func(env *vcv1.Envelope)
	blockReveal bool
	mu          sync.Mutex
}

func (c *fakeChainClient) Post(_ context.Context, payload []byte) (string, error) {
	env := &vcv1.Envelope{}
	_ = proto.Unmarshal(payload, env)

	c.mu.Lock()
	blockRev := c.blockReveal
	onPost := c.onPost
	c.mu.Unlock()

	if blockRev && env.GetReveal() != nil {
		return "", fmt.Errorf("simulated reveal drop")
	}
	id, err := c.hub.postAs(c.pubKey, payload)
	if err == nil && onPost != nil {
		onPost(env)
	}
	return id, err
}

func (c *fakeChainClient) ListOrder(_ context.Context) ([]OrderItem, error) {
	c.hub.mu.Lock()
	defer c.hub.mu.Unlock()
	return append([]OrderItem(nil), c.hub.order...), nil
}

func (c *fakeChainClient) GetMetadata(_ context.Context, id string) (ActionMeta, error) {
	c.hub.mu.Lock()
	defer c.hub.mu.Unlock()
	m, ok := c.hub.meta[id]
	if !ok {
		return ActionMeta{}, fmt.Errorf("not found")
	}
	return m, nil
}

func (c *fakeChainClient) GetPayload(_ context.Context, id string) ([]byte, error) {
	c.hub.mu.Lock()
	defer c.hub.mu.Unlock()
	p, ok := c.hub.payload[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return append([]byte(nil), p...), nil
}

func (c *fakeChainClient) LikeAuthor(_ context.Context, amount int64, authorPub string) error {
	c.hub.mu.Lock()
	defer c.hub.mu.Unlock()
	c.hub.votes = append(c.hub.votes, recordedVote{
		voter:  c.pubKey,
		like:   true,
		amount: amount,
		target: authorPub,
	})
	c.hub.reps[authorPub] += amount
	return nil
}

func (c *fakeChainClient) DislikeAuthor(_ context.Context, amount int64, authorPub string) error {
	c.hub.mu.Lock()
	defer c.hub.mu.Unlock()
	c.hub.votes = append(c.hub.votes, recordedVote{
		voter:  c.pubKey,
		like:   false,
		amount: amount,
		target: authorPub,
	})
	c.hub.reps[authorPub] -= amount
	return nil
}

func (c *fakeChainClient) RepsAuthor(_ context.Context, authorPub string) (int64, error) {
	c.hub.mu.Lock()
	defer c.hub.mu.Unlock()
	return c.hub.reps[authorPub], nil
}

func (c *fakeChainClient) SyncRecv(_ context.Context, _ string) error { return nil }
func (c *fakeChainClient) SyncSend(_ context.Context, _ string) error { return nil }

type fakeStore struct {
	mu    sync.Mutex
	seq   int
	blobs map[string][]byte
	dirs  map[string]map[string][]byte
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		blobs: make(map[string][]byte),
		dirs:  make(map[string]map[string][]byte),
	}
}

func (s *fakeStore) Add(_ context.Context, r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	cid := fmt.Sprintf("QmBlob%d", s.seq)
	s.blobs[cid] = data
	return cid, nil
}

func (s *fakeStore) AddDir(_ context.Context, dir string) (string, error) {
	files := make(map[string][]byte)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = b
		return nil
	})
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	cid := fmt.Sprintf("QmDir%d", s.seq)
	s.dirs[cid] = files
	return cid, nil
}

func (s *fakeStore) Fetch(_ context.Context, cid string, dest string) error {
	s.mu.Lock()
	files, okDir := s.dirs[cid]
	blob, okBlob := s.blobs[cid]
	s.mu.Unlock()

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if okDir {
		for rel, content := range files {
			target := filepath.Join(dest, rel)
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(target, content, 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	if okBlob {
		return os.WriteFile(filepath.Join(dest, "content"), blob, 0o644)
	}
	return fmt.Errorf("cid not found: %s", cid)
}

func (s *fakeStore) Ping(_ context.Context) error { return nil }

type fakeRunner struct {
	mu          sync.Mutex
	customFn    func(ctx context.Context, inputDir, outputDir string) (int32, error)
	replace     []byte
	cleanups    atomic.Int32
	runs        atomic.Int32
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{}
}

func newDivergentFakeRunner(replace []byte) *fakeRunner {
	return &fakeRunner{
		replace: append([]byte(nil), replace...),
	}
}

func (r *fakeRunner) Run(ctx context.Context, spec sandbox.Spec) (sandbox.Result, error) {
	r.runs.Add(1)
	cur := r.inFlight.Add(1)
	defer r.inFlight.Add(-1)
	for {
		prev := r.maxInFlight.Load()
		if cur <= prev || r.maxInFlight.CompareAndSwap(prev, cur) {
			break
		}
	}

	r.mu.Lock()
	fn := r.customFn
	replace := r.replace
	r.mu.Unlock()
	if fn != nil {
		code, err := fn(ctx, spec.InputDir, spec.OutputDir)
		return sandbox.Result{ExitCode: code}, err
	}

	entries, err := os.ReadDir(spec.InputDir)
	if err != nil || len(entries) == 0 {
		return sandbox.Result{ExitCode: 1}, fmt.Errorf("empty input dir")
	}
	data, err := os.ReadFile(filepath.Join(spec.InputDir, entries[0].Name()))
	if err != nil {
		return sandbox.Result{ExitCode: 1}, err
	}
	words := len(strings.Fields(string(data)))
	out := []byte(fmt.Sprintf("%d\n", words))
	if len(replace) > 0 {
		out = replace
	}
	if err := os.WriteFile(filepath.Join(spec.OutputDir, "result.txt"), out, 0o644); err != nil {
		return sandbox.Result{ExitCode: 1}, err
	}
	return sandbox.Result{ExitCode: 0}, nil
}

func (r *fakeRunner) EnsureImage(_ context.Context, _ sandbox.ImageRef) error { return nil }

func (r *fakeRunner) Cleanup(_ context.Context) error {
	r.cleanups.Add(1)
	return nil
}

func (r *fakeRunner) Ping(_ context.Context) error { return nil }

const (
	alicePub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAlice"
	bobPub   = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBob"
	carolPub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICarol"
)

func startTestNode(
	t *testing.T,
	id, pub string,
	stateDir string,
	chain ChainAPI,
	store Store,
	runner Runner,
	tickAfter time.Duration,
	policy *WorkerPolicy,
) *Node {
	t.Helper()
	n, err := NewNode(Config{
		ID:           id,
		StateDir:     stateDir,
		PollInterval: 15 * time.Millisecond,
		TickAfter:    tickAfter,
	}, WithPublicKey(pub), WithChain(chain), WithStore(store), WithRunner(runner))
	if err != nil {
		t.Fatalf("NewNode(%s): %v", id, err)
	}
	if policy != nil {
		if err := n.Serve(*policy); err != nil {
			t.Fatalf("Serve(%s): %v", id, err)
		}
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start(%s): %v", id, err)
	}
	t.Cleanup(func() {
		_ = n.Stop(context.Background())
	})
	return n
}

func TestSDKEndToEndHonestAndDivergent(t *testing.T) {
	for _, tc := range []struct {
		name           string
		carolDivergent bool
		wantWinners    int
		wantDivergent  []string
	}{
		{
			name:           "honest",
			carolDivergent: false,
			wantWinners:    3,
			wantDivergent:  nil,
		},
		{
			name:           "divergent",
			carolDivergent: true,
			wantWinners:    2,
			wantDivergent:  []string{carolPub},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hub := newFakeChainHub()
			store := newFakeStore()
			honestRunner := newFakeRunner()
			carolRunner := honestRunner
			if tc.carolDivergent {
				carolRunner = newDivergentFakeRunner([]byte("999\n"))
			}
			tmp := t.TempDir()

			alice := startTestNode(t, "alice", alicePub, filepath.Join(tmp, "alice-state"), hub.clientFor(alicePub), store, honestRunner, 2*time.Second, &WorkerPolicy{
				AllowAnyImage: true,
				AllowOwnJobs:  true,
			})
			_ = startTestNode(t, "bob", bobPub, filepath.Join(tmp, "bob-state"), hub.clientFor(bobPub), store, honestRunner, 2*time.Second, &WorkerPolicy{
				AllowAnyImage: true,
			})
			_ = startTestNode(t, "carol", carolPub, filepath.Join(tmp, "carol-state"), hub.clientFor(carolPub), store, carolRunner, 2*time.Second, &WorkerPolicy{
				AllowAnyImage: true,
			})

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			job, err := alice.Submit(ctx, JobSpec{
				Image:    testAlpineImage,
				Cmd:      []string{"sh", "-c", "wc -w < /input/data.txt > /output/result.txt"},
				Input:    FromBytes("data.txt", []byte("volunteer computing on freechains ipfs and podman works end to end\n")),
				Replicas: 3,
				Quorum:   2,
				Reward:   1000,
			})
			if err != nil {
				t.Fatalf("Submit failed: %v", err)
			}

			res, err := job.Wait(ctx)
			if err != nil {
				t.Fatalf("Wait failed: %v", err)
			}
			if len(res.Winners) != tc.wantWinners {
				t.Fatalf("expected %d winners, got %v", tc.wantWinners, res.Winners)
			}
			if !reflect.DeepEqual(res.Divergent, tc.wantDivergent) {
				t.Fatalf("expected divergent %v, got %v", tc.wantDivergent, res.Divergent)
			}

			outDir := filepath.Join(tmp, "fetched")
			if err := alice.Fetch(ctx, res.OutputCID, outDir); err != nil {
				t.Fatalf("Fetch failed: %v", err)
			}
			b, err := os.ReadFile(filepath.Join(outDir, "result.txt"))
			if err != nil {
				t.Fatalf("ReadFile result.txt: %v", err)
			}
			if strings.TrimSpace(string(b)) != "11" {
				t.Fatalf("expected result 11, got %q", string(b))
			}

			// Verify no self-vote occurred.
			for _, v := range hub.getVotes() {
				if v.voter == v.target {
					t.Fatalf("self-vote detected: %+v", v)
				}
			}
		})
	}
}

func TestTwoConcurrentJobsOnSameChain(t *testing.T) {
	hub := newFakeChainHub()
	store := newFakeStore()
	runnerB := newFakeRunner()
	runnerC := newFakeRunner()
	tmp := t.TempDir()

	alice := startTestNode(t, "alice", alicePub, filepath.Join(tmp, "alice-state"), hub.clientFor(alicePub), store, newFakeRunner(), 2*time.Second, nil)
	_ = startTestNode(t, "bob", bobPub, filepath.Join(tmp, "bob-state"), hub.clientFor(bobPub), store, runnerB, 2*time.Second, &WorkerPolicy{
		AllowImages:   []string{testAlpineImage},
		MaxConcurrent: 1,
	})
	_ = startTestNode(t, "carol", carolPub, filepath.Join(tmp, "carol-state"), hub.clientFor(carolPub), store, runnerC, 2*time.Second, &WorkerPolicy{
		AllowImages:   []string{testAlpineImage},
		MaxConcurrent: 1,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job1, err := alice.Submit(ctx, JobSpec{
		Image:    testAlpineImage,
		Cmd:      []string{"wc", "-w"},
		Input:    FromBytes("data.txt", []byte("one two three\n")),
		Replicas: 2,
		Quorum:   2,
		Reward:   500,
	})
	if err != nil {
		t.Fatalf("Submit job1: %v", err)
	}

	job2, err := alice.Submit(ctx, JobSpec{
		Image:    testAlpineImage,
		Cmd:      []string{"wc", "-w"},
		Input:    FromBytes("data.txt", []byte("alpha beta gamma delta epsilon\n")),
		Replicas: 2,
		Quorum:   2,
		Reward:   500,
	})
	if err != nil {
		t.Fatalf("Submit job2: %v", err)
	}

	res1, err := job1.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait job1: %v", err)
	}
	res2, err := job2.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait job2: %v", err)
	}

	if res1.JobID == res2.JobID || res1.AcceptedHash == res2.AcceptedHash {
		t.Fatalf("expected distinct job IDs and hashes: res1=%+v res2=%+v", res1, res2)
	}
	if runnerB.maxInFlight.Load() > 1 || runnerC.maxInFlight.Load() > 1 {
		t.Fatalf("expected MaxConcurrent=1 to bound in-flight runs, got bob=%d carol=%d", runnerB.maxInFlight.Load(), runnerC.maxInFlight.Load())
	}
}

func TestWorkerNeverRevealsFinalizesViaTicks(t *testing.T) {
	hub := newFakeChainHub()
	store := newFakeStore()
	runner := newFakeRunner()
	tmp := t.TempDir()

	carolClient := hub.clientFor(carolPub)
	carolClient.blockReveal = true

	alice := startTestNode(t, "alice", alicePub, filepath.Join(tmp, "alice-state"), hub.clientFor(alicePub), store, runner, 40*time.Millisecond, &WorkerPolicy{
		AllowAnyImage: true,
		AllowOwnJobs:  true,
	})
	_ = startTestNode(t, "bob", bobPub, filepath.Join(tmp, "bob-state"), hub.clientFor(bobPub), store, runner, 40*time.Millisecond, &WorkerPolicy{
		AllowAnyImage: true,
	})
	_ = startTestNode(t, "carol", carolPub, filepath.Join(tmp, "carol-state"), carolClient, store, runner, 2*time.Second, &WorkerPolicy{
		AllowAnyImage: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job, err := alice.Submit(ctx, JobSpec{
		Image:        testAlpineImage,
		Cmd:          []string{"wc", "-w"},
		Input:        FromBytes("data.txt", []byte("hello world\n")),
		Replicas:     3,
		Quorum:       2,
		Reward:       1000,
		CommitWindow: 10,
		RevealWindow: 4,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	res, err := job.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !reflect.DeepEqual(res.NoReveal, []string{carolPub}) {
		t.Fatalf("expected carol in NoReveal, got %v", res.NoReveal)
	}
	if len(res.Winners) != 2 {
		t.Fatalf("expected 2 winners, got %v", res.Winners)
	}
}

func TestPersistenceRestartAndMissingOutputDir(t *testing.T) {
	t.Run("worker killed after commit and restarted reveals with same nonce without second commit", func(t *testing.T) {
		hub := newFakeChainHub()
		store := newFakeStore()
		runner := newFakeRunner()
		tmp := t.TempDir()

		bobStateDir := filepath.Join(tmp, "bob-state")
		bobClient := hub.clientFor(bobPub)
		bobCommitted := make(chan string, 1)
		bobClient.onPost = func(env *vcv1.Envelope) {
			if env.GetCommit() != nil {
				select {
				case bobCommitted <- env.JobId:
				default:
				}
			}
		}

		// Block carol initially so the job stays in Committing while bob commits and crashes.
		hub.setBlocked(carolPub, true)

		alice := startTestNode(t, "alice", alicePub, filepath.Join(tmp, "alice-state"), hub.clientFor(alicePub), store, runner, 2*time.Second, &WorkerPolicy{
			AllowAnyImage: true,
			AllowOwnJobs:  true,
		})
		bob := startTestNode(t, "bob", bobPub, bobStateDir, bobClient, store, runner, 2*time.Second, &WorkerPolicy{
			AllowAnyImage: true,
		})
		_ = startTestNode(t, "carol", carolPub, filepath.Join(tmp, "carol-state"), hub.clientFor(carolPub), store, runner, 2*time.Second, &WorkerPolicy{
			AllowAnyImage: true,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		job, err := alice.Submit(ctx, JobSpec{
			Image:    testAlpineImage,
			Cmd:      []string{"wc", "-w"},
			Input:    FromBytes("data.txt", []byte("one two three four\n")),
			Replicas: 3,
			Quorum:   2,
			Reward:   500,
		})
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}

		var committedJobID string
		select {
		case committedJobID = <-bobCommitted:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for bob commit")
		}

		persistedBefore, err := actors.LoadTaskState(bobStateDir, committedJobID)
		if err != nil || persistedBefore == nil {
			t.Fatalf("expected persisted state for bob before restart, got %v, %v", persistedBefore, err)
		}

		// Kill bob's TaskActor under the supervisor AND also test node restart with same StateDir.
		if err := bob.KillTaskActorForTest(committedJobID); err != nil {
			t.Fatalf("KillTaskActorForTest: %v", err)
		}
		time.Sleep(40 * time.Millisecond)
		_ = bob.Stop(ctx)

		bob2 := startTestNode(t, "bob2", bobPub, bobStateDir, hub.clientFor(bobPub), store, runner, 2*time.Second, &WorkerPolicy{
			AllowAnyImage: true,
		})
		_ = bob2
		time.Sleep(40 * time.Millisecond)

		if got := hub.countCommitsBy(bobPub); got != 1 {
			t.Fatalf("expected exactly 1 commit from bob after restart, got %d", got)
		}

		// Unblock carol so the 3rd commit arrives and Revealing opens.
		hub.setBlocked(carolPub, false)

		res, err := job.Wait(ctx)
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		if len(res.Winners) != 3 {
			t.Fatalf("expected all 3 workers (including restarted bob) to win, got %v", res.Winners)
		}

		// Wait briefly for bob2 to observe JobFinalize and remove its state file.
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			st, _ := actors.LoadTaskState(bobStateDir, committedJobID)
			if st == nil {
				break
			}
			time.Sleep(15 * time.Millisecond)
		}
		if st, _ := actors.LoadTaskState(bobStateDir, committedJobID); st != nil {
			t.Fatalf("expected persisted state to be removed after JobFinalize, still present: %+v", st)
		}
	})

	t.Run("missing output directory at reveal time re-runs and checks digest", func(t *testing.T) {
		for _, matchDigest := range []bool{true, false} {
			hub := newFakeChainHub()
			store := newFakeStore()
			bobRunner := newFakeRunner()
			otherRunner := newFakeRunner()
			tmp := t.TempDir()

			bobStateDir := filepath.Join(tmp, "bob-state")
			bobClient := hub.clientFor(bobPub)
			bobCommitted := make(chan string, 1)
			bobClient.onPost = func(env *vcv1.Envelope) {
				if env.GetCommit() != nil {
					select {
					case bobCommitted <- env.JobId:
					default:
					}
				}
			}

			hub.setBlocked(carolPub, true)

			alice := startTestNode(t, "alice", alicePub, filepath.Join(tmp, "alice-state"), hub.clientFor(alicePub), store, otherRunner, 40*time.Millisecond, &WorkerPolicy{
				AllowAnyImage: true,
				AllowOwnJobs:  true,
			})
			_ = startTestNode(t, "bob", bobPub, bobStateDir, bobClient, store, bobRunner, 2*time.Second, &WorkerPolicy{
				AllowAnyImage: true,
			})
			_ = startTestNode(t, "carol", carolPub, filepath.Join(tmp, "carol-state"), hub.clientFor(carolPub), store, otherRunner, 40*time.Millisecond, &WorkerPolicy{
				AllowAnyImage: true,
			})

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			job, err := alice.Submit(ctx, JobSpec{
				Image:        testAlpineImage,
				Cmd:          []string{"wc", "-w"},
				Input:        FromBytes("data.txt", []byte("a b c\n")),
				Replicas:     3,
				Quorum:       2,
				Reward:       500,
				CommitWindow: 10,
				RevealWindow: 4,
			})
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}

			jobID := <-bobCommitted
			st, err := actors.LoadTaskState(bobStateDir, jobID)
			if err != nil || st == nil {
				t.Fatalf("LoadTaskState: %v", err)
			}

			// Remove bob's output directory before Revealing opens!
			if err := os.RemoveAll(st.OutputDir); err != nil {
				t.Fatalf("RemoveAll outputDir: %v", err)
			}

			if !matchDigest {
				bobRunner.mu.Lock()
				bobRunner.customFn = func(_ context.Context, _, outputDir string) (int32, error) {
					return 0, os.WriteFile(filepath.Join(outputDir, "result.txt"), []byte("tampered-on-rerun\n"), 0o644)
				}
				bobRunner.mu.Unlock()
			}

			hub.setBlocked(carolPub, false)

			res, err := job.Wait(ctx)
			if err != nil {
				t.Fatalf("Wait: %v", err)
			}
			if matchDigest {
				if len(res.Winners) != 3 {
					t.Fatalf("expected bob to reveal when re-run digest matches, got winners=%v no_reveal=%v", res.Winners, res.NoReveal)
				}
			} else {
				if !reflect.DeepEqual(res.NoReveal, []string{bobPub}) {
					t.Fatalf("expected bob to skip reveal when re-run digest mismatches, got no_reveal=%v", res.NoReveal)
				}
			}
		}
	})
}

func TestPolicyAllowlistOwnJobsAndRepsPreCheck(t *testing.T) {
	t.Run("insufficient reps fails pre-check", func(t *testing.T) {
		hub := newFakeChainHub()
		hub.reps[alicePub] = 1500 // Needs 1000*3 + 2*500 = 4000
		store := newFakeStore()
		runner := newFakeRunner()

		alice := startTestNode(t, "alice", alicePub, t.TempDir(), hub.clientFor(alicePub), store, runner, time.Second, nil)
		_, err := alice.Submit(context.Background(), JobSpec{
			Image:    testAlpineImage,
			Cmd:      []string{"wc", "-w"},
			Input:    FromBytes("data.txt", []byte("hi\n")),
			Replicas: 3,
			Quorum:   2,
			Reward:   1000,
		})
		if !errors.Is(err, ErrInsufficientReps) {
			t.Fatalf("expected ErrInsufficientReps, got %v", err)
		}
	})

	t.Run("own job skipped by default and image allowlist enforced", func(t *testing.T) {
		hub := newFakeChainHub()
		store := newFakeStore()
		aliceRunner := newFakeRunner()
		bobRunner := newFakeRunner()
		carolRunner := newFakeRunner()
		tmp := t.TempDir()

		// Alice has AllowOwnJobs=false (default); Bob has empty AllowImages (rejects all); Carol allows testAlpineImage.
		alice := startTestNode(t, "alice", alicePub, filepath.Join(tmp, "a"), hub.clientFor(alicePub), store, aliceRunner, 40*time.Millisecond, &WorkerPolicy{
			AllowAnyImage: true,
			AllowOwnJobs:  false,
		})
		_ = startTestNode(t, "bob", bobPub, filepath.Join(tmp, "b"), hub.clientFor(bobPub), store, bobRunner, 40*time.Millisecond, &WorkerPolicy{
			AllowImages:   nil,
			AllowAnyImage: false,
		})
		_ = startTestNode(t, "carol", carolPub, filepath.Join(tmp, "c"), hub.clientFor(carolPub), store, carolRunner, 40*time.Millisecond, &WorkerPolicy{
			AllowImages: []string{testAlpineImage},
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		job, err := alice.Submit(ctx, JobSpec{
			Image:        testAlpineImage,
			Cmd:          []string{"wc", "-w"},
			Input:        FromBytes("data.txt", []byte("hello world\n")),
			Replicas:     1,
			Quorum:       1,
			Reward:       500,
			CommitWindow: 5,
			RevealWindow: 5,
		})
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}

		res, err := job.Wait(ctx)
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		if !reflect.DeepEqual(res.Winners, []string{carolPub}) {
			t.Fatalf("expected only carol to work and win, got %v", res.Winners)
		}
		if aliceRunner.runs.Load() != 0 {
			t.Fatalf("expected alice not to run own job when AllowOwnJobs=false, runs=%d", aliceRunner.runs.Load())
		}
		if bobRunner.runs.Load() != 0 {
			t.Fatalf("expected bob not to run unallowed image, runs=%d", bobRunner.runs.Load())
		}
	})
}

func TestVotesBySubmitterWhenNonSubmitterFinalizes(t *testing.T) {
	hub := newFakeChainHub()
	store := newFakeStore()
	runner := newFakeRunner()
	tmp := t.TempDir()

	// Block alice from posting JobFinalize so a non-submitter posts JobFinalize first.
	aliceClient := hub.clientFor(alicePub)
	aliceClient.onPost = func(env *vcv1.Envelope) {
		if env.GetAnnounce() != nil {
			// Once alice announces, block alice's further posts; we will inject bob's JobFinalize when Revealing completes.
			hub.setBlocked(alicePub, true)
		}
	}

	alice := startTestNode(t, "alice", alicePub, filepath.Join(tmp, "a"), aliceClient, store, runner, 2*time.Second, nil)
	_ = startTestNode(t, "bob", bobPub, filepath.Join(tmp, "b"), hub.clientFor(bobPub), store, runner, 2*time.Second, &WorkerPolicy{
		AllowAnyImage: true,
	})
	_ = startTestNode(t, "carol", carolPub, filepath.Join(tmp, "c"), hub.clientFor(carolPub), store, newDivergentFakeRunner([]byte("999\n")), 2*time.Second, &WorkerPolicy{
		AllowAnyImage: true,
	})
	// Add a 3rd worker so N=3, Q=2 completes without alice working.
	davePub := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDave"
	hub.reps[davePub] = 20000
	_ = startTestNode(t, "dave", davePub, filepath.Join(tmp, "d"), hub.clientFor(davePub), store, runner, 2*time.Second, &WorkerPolicy{
		AllowAnyImage: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job, err := alice.Submit(ctx, JobSpec{
		Image:    testAlpineImage,
		Cmd:      []string{"wc", "-w"},
		Input:    FromBytes("data.txt", []byte("one two three\n")),
		Replicas: 3,
		Quorum:   2,
		Reward:   750,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Wait until 3 reveals are on the chain, then have bob post JobFinalize directly.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		order, _ := hub.clientFor(bobPub).ListOrder(ctx)
		var entries []protocol.Entry
		for i, item := range order {
			m, _ := hub.clientFor(bobPub).GetMetadata(ctx, item.ID)
			p, _ := hub.clientFor(bobPub).GetPayload(ctx, item.ID)
			env := &vcv1.Envelope{}
			if proto.Unmarshal(p, env) == nil {
				entries = append(entries, protocol.Entry{Index: i, Author: m.Signer, Envelope: env})
			}
		}
		st := protocol.Replay(entries)
		if st.Finalizable && st.Finalize == nil {
			finBytes, _ := proto.Marshal(&vcv1.Envelope{
				Version: 1,
				JobId:   st.JobID,
				Body: &vcv1.Envelope_Finalize{
					Finalize: &vcv1.JobFinalize{
						AcceptedHash: st.Verdict.AcceptedHash,
						Winners:      st.Verdict.Winners,
						Divergent:    st.Verdict.Divergent,
						NoReveal:     st.Verdict.NoReveal,
					},
				},
			})
			_, _ = hub.postAs(bobPub, finBytes)
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	res, err := job.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if len(res.Winners) != 2 || !reflect.DeepEqual(res.Divergent, []string{carolPub}) {
		t.Fatalf("unexpected verdict: %+v", res)
	}

	votes := hub.getVotes()
	if len(votes) != 3 {
		t.Fatalf("expected 3 votes cast by submitter alice, got %+v", votes)
	}
	for _, v := range votes {
		if v.voter != alicePub {
			t.Fatalf("expected only alice to vote, got %+v", v)
		}
		if v.amount != 750 {
			t.Fatalf("expected vote amount 750, got %+v", v)
		}
		if v.target == carolPub && v.like {
			t.Fatalf("expected dislike for divergent carol, got %+v", v)
		}
		if (v.target == bobPub || v.target == davePub) && !v.like {
			t.Fatalf("expected like for winner %s, got %+v", v.target, v)
		}
	}
}

func TestStopMidJobCancelsContainerAndKeepsPersistedState(t *testing.T) {
	hub := newFakeChainHub()
	store := newFakeStore()
	runner := newFakeRunner()
	tmp := t.TempDir()

	startedContainer := make(chan struct{}, 1)
	runner.customFn = func(ctx context.Context, _, _ string) (int32, error) {
		select {
		case startedContainer <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return -1, ctx.Err()
	}

	// Pre-populate a persisted state for another unfinished job to verify Stop keeps it.
	stateDir := filepath.Join(tmp, "worker-state")
	if err := actors.SaveTaskState(stateDir, actors.PersistedTaskState{
		JobID:      "existing-unfinished-job",
		Nonce:      []byte("nonce"),
		ResultHash: bytes.Repeat([]byte{1}, 32),
		OutputDir:  filepath.Join(tmp, "out"),
	}); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	alice := startTestNode(t, "alice", alicePub, stateDir, hub.clientFor(alicePub), store, runner, 2*time.Second, &WorkerPolicy{
		AllowAnyImage: true,
		AllowOwnJobs:  true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job, err := alice.Submit(ctx, JobSpec{
		Image:    testAlpineImage,
		Cmd:      []string{"sleep", "60"},
		Input:    FromBytes("data.txt", []byte("input\n")),
		Replicas: 1,
		Quorum:   1,
		Reward:   100,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	select {
	case <-startedContainer:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for container to start")
	}

	cleanupsBefore := runner.cleanups.Load()
	if err := alice.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if runner.cleanups.Load() <= cleanupsBefore {
		t.Fatalf("expected runner.Cleanup to be called on Stop")
	}

	_, err = job.Wait(ctx)
	if !errors.Is(err, ErrNodeStopped) {
		t.Fatalf("expected Wait to fail with ErrNodeStopped after Stop, got %v", err)
	}

	persisted, err := actors.LoadTaskState(stateDir, "existing-unfinished-job")
	if err != nil || persisted == nil {
		t.Fatalf("expected Stop to preserve persisted state for unfinished jobs, got %v, %v", persisted, err)
	}

	_ = canon.Limits{MaxBytes: 10 * 1024 * 1024, MaxFiles: 100}
}

func TestEventsNeverBlocksWhenUnread(t *testing.T) {
	job := newJob("test-job")
	total := eventBufferCapacity + 50
	for i := 0; i < total; i++ {
		job.pushEvent(Committed{JobID: "test-job", Worker: fmt.Sprintf("worker-%d", i)})
	}
	if job.DroppedEvents() != 50 {
		t.Fatalf("expected 50 dropped events, got %d", job.DroppedEvents())
	}
	first := <-job.Events()
	c, ok := first.(Committed)
	if !ok || c.Worker != "worker-50" {
		t.Fatalf("expected oldest kept event to be worker-50, got %#v", first)
	}
}
