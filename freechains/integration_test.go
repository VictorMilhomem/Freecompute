package freechains

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestIntegration(t *testing.T) {
	fcPath, err := exec.LookPath("freechains")
	if err != nil {
		t.Skip("freechains binary not found in PATH, skipping integration test")
	}

	tmpDir := t.TempDir()
	chainsDir := filepath.Join(tmpDir, "chains")
	if err := os.MkdirAll(chainsDir, 0755); err != nil {
		t.Fatalf("failed to create chains dir: %v", err)
	}

	keyPath := filepath.Join(tmpDir, "pioneer_key")
	pubKeyPath := keyPath + ".pub"

	// Generate ed25519 keypair for testing
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", keyPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to generate key: %s, %v", string(out), err)
	}

	runner := NewExecRunner()
	serialRunner := NewSerialRunner(runner)

	client, err := NewClient(serialRunner, Config{
		Binary:         fcPath,
		Root:           tmpDir,
		DefaultKey:     PrivateKeyPath(keyPath),
		CommandTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	alias := Alias("#testchat")

	// 1. Initialize chain with pioneer key
	chainID, err := client.ChainsAddInit(ctx, alias, PublicKey(pubKeyPath))
	if err != nil {
		t.Fatalf("ChainsAddInit failed: %v", err)
	}
	if chainID == "" || chainID[0] != '#' {
		t.Errorf("unexpected chain ID format: %s", chainID)
	}

	// 2. Verify ChainsDir
	aliases, err := client.ChainsDir(ctx)
	if err != nil {
		t.Fatalf("ChainsDir failed: %v", err)
	}
	found := false
	for _, a := range aliases {
		if a == alias {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("alias %s not found in ChainsDir: %v", alias, aliases)
	}

	// 3. Post signed inline message
	msg := "hello freechains integration"
	postID, err := client.Post(ctx, alias, InlinePayload(msg), PostOptions{})
	if err != nil {
		t.Fatalf("Post with default key failed: %v", err)
	}
	if err := postID.Validate(); err != nil {
		t.Fatalf("invalid post action ID: %v", err)
	}

	// 4. Post with --beg
	begMsg := "begged message"
	begID, err := client.Post(ctx, alias, InlinePayload(begMsg), PostOptions{Beg: true})
	if err != nil {
		t.Fatalf("Post with Beg failed: %v", err)
	}

	// 5. Verify ListBegs contains begID
	begs, err := client.ListBegs(ctx, alias)
	if err != nil {
		t.Fatalf("ListBegs failed: %v", err)
	}
	foundBeg := false
	for _, id := range begs {
		if id == begID {
			foundBeg = true
			break
		}
	}
	if !foundBeg {
		t.Errorf("beg ID %s not found in ListBegs: %v", begID, begs)
	}

	// 6. Verify ListOrder contains postID
	order, err := client.ListOrder(ctx, alias)
	if err != nil {
		t.Fatalf("ListOrder failed: %v", err)
	}
	foundPost := false
	for _, entry := range order {
		if entry.ID == postID && !entry.Revoked {
			foundPost = true
			break
		}
	}
	if !foundPost {
		t.Errorf("post ID %s not found unrevoked in ListOrder: %+v", postID, order)
	}

	// 7. GetPayload
	payloadBytes, err := client.GetPayload(ctx, alias, postID)
	if err != nil {
		t.Fatalf("GetPayload failed: %v", err)
	}
	if string(payloadBytes) != msg+"\n" && string(payloadBytes) != msg {
		t.Errorf("unexpected payload content: %q", string(payloadBytes))
	}

	// 8. GetMetadata
	meta, err := client.GetMetadata(ctx, alias, postID)
	if err != nil {
		t.Fatalf("GetMetadata failed: %v", err)
	}
	if meta.ID != postID {
		t.Errorf("metadata ID mismatch: got %s, want %s", meta.ID, postID)
	}
	if meta.Kind != ActionPost {
		t.Errorf("expected kind post, got %s", meta.Kind)
	}
	if meta.Time <= 0 {
		t.Errorf("expected positive metadata time, got %d", meta.Time)
	}

	// 9. Like action
	likeID, err := client.Like(ctx, alias, 1000, ActionTarget(postID), VoteOptions{})
	if err != nil {
		t.Fatalf("Like action failed: %v", err)
	}
	if err := likeID.Validate(); err != nil {
		t.Fatalf("invalid like action ID: %v", err)
	}

	// 10. Revoke action
	revokeID, err := client.Revoke(ctx, alias, 1000, postID, VoteOptions{})
	if err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}
	if err := revokeID.Validate(); err != nil {
		t.Fatalf("invalid revoke action ID: %v", err)
	}

	// 11. Check ListOrder has postID revoked
	order, err = client.ListOrder(ctx, alias)
	if err != nil {
		t.Fatalf("ListOrder after revoke failed: %v", err)
	}
	foundRevoked := false
	for _, entry := range order {
		if entry.ID == postID && entry.Revoked {
			foundRevoked = true
			break
		}
	}
	if !foundRevoked {
		t.Errorf("post ID %s not marked revoked in ListOrder: %+v", postID, order)
	}

	// 12. Check ListRevokes
	revokes, err := client.ListRevokes(ctx, alias)
	if err != nil {
		t.Fatalf("ListRevokes failed: %v", err)
	}
	foundRevokeList := false
	for _, id := range revokes {
		if id == postID {
			foundRevokeList = true
			break
		}
	}
	if !foundRevokeList {
		t.Errorf("post ID %s not in ListRevokes: %v", postID, revokes)
	}

	// 13. GetPayload on revoked post must return ErrPayloadRevoked
	_, err = client.GetPayload(ctx, alias, postID)
	if !errors.Is(err, ErrPayloadRevoked) {
		t.Errorf("expected ErrPayloadRevoked for revoked post, got %v", err)
	}

	// 14. Reps queries
	actionReps, err := client.RepsActions(ctx, alias)
	if err != nil {
		t.Fatalf("RepsActions failed: %v", err)
	}
	if len(actionReps) == 0 {
		t.Error("expected non-empty action reps")
	}

	singleRep, err := client.RepsAction(ctx, alias, postID)
	if err != nil {
		t.Fatalf("RepsAction failed: %v", err)
	}
	_ = singleRep

	// 15. ChainsRem
	if err := client.ChainsRem(ctx, alias); err != nil {
		t.Fatalf("ChainsRem failed: %v", err)
	}

	// Verify chain is removed
	aliases, err = client.ChainsDir(ctx)
	if err != nil {
		t.Fatalf("ChainsDir after rem failed: %v", err)
	}
	for _, a := range aliases {
		if a == alias {
			t.Errorf("chain %s still present after ChainsRem", alias)
		}
	}
}
