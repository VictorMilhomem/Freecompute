package chainbridge

import (
	"context"
	"fmt"

	"github.com/VictorMilhomem/freecompute/freechains"
)

// SyncResult records a peer synchronization error.
type SyncResult struct {
	Peer string
	Err  error
}

// Syncer iterates configured peers and synchronizes chain data.
type Syncer struct {
	api   ChainAPI
	alias freechains.Alias
	peers []string
}

// NewSyncer constructs a new Syncer.
func NewSyncer(api ChainAPI, alias freechains.Alias, peers []string) *Syncer {
	peerList := make([]string, len(peers))
	copy(peerList, peers)
	return &Syncer{
		api:   api,
		alias: alias,
		peers: peerList,
	}
}

// SyncAll runs SyncRecv followed by SyncSend against each peer, collecting failures without aborting.
func (s *Syncer) SyncAll(ctx context.Context) []SyncResult {
	var failures []SyncResult
	for _, peer := range s.peers {
		var peerErr error
		if err := s.api.SyncRecv(ctx, s.alias, peer); err != nil {
			peerErr = fmt.Errorf("sync recv failed: %w", err)
		} else if err := s.api.SyncSend(ctx, s.alias, peer); err != nil {
			peerErr = fmt.Errorf("sync send failed: %w", err)
		}

		if peerErr != nil {
			failures = append(failures, SyncResult{
				Peer: peer,
				Err:  peerErr,
			})
		}
	}
	return failures
}
