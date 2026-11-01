package freecompute

import (
	"log/slog"

	"github.com/VictorMilhomem/freecompute/internal/actors"
	"github.com/VictorMilhomem/freecompute/internal/sandbox"
)

type Store = actors.Store
type Runner = sandbox.Runner
type ChainAPI = actors.ChainAPI
type OrderItem = actors.OrderItem
type ActionMeta = actors.ActionMeta

type Option func(*Node)

func WithStore(s Store) Option {
	return func(n *Node) {
		n.store = s
	}
}

func WithRunner(r Runner) Option {
	return func(n *Node) {
		n.sbRunner = r
	}
}

func WithChain(c ChainAPI) Option {
	return func(n *Node) {
		n.chain = c
	}
}

func WithLogger(l *slog.Logger) Option {
	return func(n *Node) {
		if l != nil {
			n.logger = l
		}
	}
}

func WithPostCost(cost int64) Option {
	return func(n *Node) {
		if cost >= 0 {
			n.postCost = cost
		}
	}
}

func WithPublicKey(pubKey string) Option {
	return func(n *Node) {
		n.pubKey = pubKey
	}
}

func WithMaxTicks(max int) Option {
	return func(n *Node) {
		if max > 0 {
			n.maxTicks = max
		}
	}
}
