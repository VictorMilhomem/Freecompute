package freechains

import (
	"reflect"
	"testing"
)

func TestArgsBuilder(t *testing.T) {
	alias := Alias("#chat")
	actID := ActionID("3519b231c31e34e6afcb25367a87e232792693e6")
	pubKey := PublicKey("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKhr2Sz7h+tgvcbcDw1qoLGU0IEsvz+sc0qgA6swK5v5")
	keyPath := PrivateKeyPath("/home/user/.ssh/fc_key")

	tests := []struct {
		name     string
		cfg      Config
		buildFn  func(b *argsBuilder) ([]string, error)
		expected []string
	}{
		{
			name: "daemon start default",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.daemonStart(0, false, nil), nil
			},
			expected: []string{"freechains", "daemon", "start"},
		},
		{
			name: "daemon start with root, now, port, hub, and git-opts",
			cfg:  Config{Binary: "freechains", Root: "/var/fc", Now: 1780000000},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.daemonStart(8331, true, []string{"--verbose", "--export-all"}), nil
			},
			expected: []string{
				"freechains",
				"--root=/var/fc",
				"--now=1780000000",
				"daemon",
				"start",
				"--port=8331",
				"--hub",
				"--",
				"--verbose",
				"--export-all",
			},
		},
		{
			name: "daemon stop with root",
			cfg:  Config{Binary: "freechains", Root: "/var/fc"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.daemonStop(), nil
			},
			expected: []string{"freechains", "--root=/var/fc", "daemon", "stop"},
		},
		{
			name: "chains dir",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.chainsDir(), nil
			},
			expected: []string{"freechains", "chains", "dir"},
		},
		{
			name: "chains add init without pioneers",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.chainsAddInit(alias)
			},
			expected: []string{"freechains", "chains", "add", "#chat", "init"},
		},
		{
			name: "chains add init with repeated pioneers",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				p1 := PublicKey("ssh-ed25519 AAA1")
				p2 := PublicKey("/path/to/p2.pub")
				return b.chainsAddInit(alias, p1, p2)
			},
			expected: []string{
				"freechains",
				"chains",
				"add",
				"#chat",
				"init",
				"--pioneer=ssh-ed25519 AAA1",
				"--pioneer=/path/to/p2.pub",
			},
		},
		{
			name: "chains add clone",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.chainsAddClone(alias, "localhost:8330/#chat")
			},
			expected: []string{"freechains", "chains", "add", "#chat", "clone", "localhost:8330/#chat"},
		},
		{
			name: "chains rem",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.chainsRem(alias)
			},
			expected: []string{"freechains", "chains", "rem", "#chat"},
		},
		{
			name: "chain post inline with beg",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.post(alias, InlinePayload("hello"), PostOptions{Beg: true})
			},
			expected: []string{"freechains", "chain", "#chat", "post", "inline", "hello", "--beg"},
		},
		{
			name: "chain post inline with sign",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.post(alias, InlinePayload("hello"), PostOptions{Sign: keyPath})
			},
			expected: []string{"freechains", "chain", "#chat", "post", "inline", "hello", "--sign=/home/user/.ssh/fc_key"},
		},
		{
			name: "chain post file with beg",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.post(alias, FilePayload("/tmp/payload.txt"), PostOptions{Beg: true})
			},
			expected: []string{"freechains", "chain", "#chat", "post", "file", "/tmp/payload.txt", "--beg"},
		},
		{
			name: "chain like action with sign, why, and file",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.like(alias, 1000, ActionTarget(actID), VoteOptions{
					Sign: keyPath,
					Why:  "great work",
					File: "/tmp/original.txt",
				})
			},
			expected: []string{
				"freechains",
				"chain",
				"#chat",
				"like",
				"1000",
				"action",
				"3519b231c31e34e6afcb25367a87e232792693e6",
				"--sign=/home/user/.ssh/fc_key",
				"--why=great work",
				"--file=/tmp/original.txt",
			},
		},
		{
			name: "chain like author",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.like(alias, 500, AuthorTarget(pubKey), VoteOptions{
					Sign: keyPath,
				})
			},
			expected: []string{
				"freechains",
				"chain",
				"#chat",
				"like",
				"500",
				"author",
				string(pubKey),
				"--sign=/home/user/.ssh/fc_key",
			},
		},
		{
			name: "chain dislike action",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.dislike(alias, 200, ActionTarget(actID), VoteOptions{
					Sign: keyPath,
					Why:  "spam",
				})
			},
			expected: []string{
				"freechains",
				"chain",
				"#chat",
				"dislike",
				"200",
				"action",
				"3519b231c31e34e6afcb25367a87e232792693e6",
				"--sign=/home/user/.ssh/fc_key",
				"--why=spam",
			},
		},
		{
			name: "chain dislike author",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.dislike(alias, 200, AuthorTarget(pubKey), VoteOptions{
					Sign: keyPath,
				})
			},
			expected: []string{
				"freechains",
				"chain",
				"#chat",
				"dislike",
				"200",
				"author",
				string(pubKey),
				"--sign=/home/user/.ssh/fc_key",
			},
		},
		{
			name: "chain revoke",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.revoke(alias, 1000, actID, VoteOptions{
					Sign: keyPath,
					Why:  "illegal content",
				})
			},
			expected: []string{
				"freechains",
				"chain",
				"#chat",
				"revoke",
				"1000",
				"3519b231c31e34e6afcb25367a87e232792693e6",
				"--sign=/home/user/.ssh/fc_key",
				"--why=illegal content",
			},
		},
		{
			name: "chain unrevoke with file",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.unrevoke(alias, 1000, actID, VoteOptions{
					Sign: keyPath,
					Why:  "restored",
					File: "/tmp/restore.txt",
				})
			},
			expected: []string{
				"freechains",
				"chain",
				"#chat",
				"unrevoke",
				"1000",
				"3519b231c31e34e6afcb25367a87e232792693e6",
				"--sign=/home/user/.ssh/fc_key",
				"--why=restored",
				"--file=/tmp/restore.txt",
			},
		},
		{
			name: "chain list order",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.list(alias, "order")
			},
			expected: []string{"freechains", "chain", "#chat", "list", "order"},
		},
		{
			name: "chain list dag",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.list(alias, "dag")
			},
			expected: []string{"freechains", "chain", "#chat", "list", "dag"},
		},
		{
			name: "chain list begs",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.list(alias, "begs")
			},
			expected: []string{"freechains", "chain", "#chat", "list", "begs"},
		},
		{
			name: "chain list revokes",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.list(alias, "revokes")
			},
			expected: []string{"freechains", "chain", "#chat", "list", "revokes"},
		},
		{
			name: "chain get metadata",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.get(alias, "metadata", actID)
			},
			expected: []string{"freechains", "chain", "#chat", "get", "metadata", "3519b231c31e34e6afcb25367a87e232792693e6"},
		},
		{
			name: "chain get payload",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.get(alias, "payload", actID)
			},
			expected: []string{"freechains", "chain", "#chat", "get", "payload", "3519b231c31e34e6afcb25367a87e232792693e6"},
		},
		{
			name: "chain reps actions",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.repsCategory(alias, "actions")
			},
			expected: []string{"freechains", "chain", "#chat", "reps", "actions"},
		},
		{
			name: "chain reps authors",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.repsCategory(alias, "authors")
			},
			expected: []string{"freechains", "chain", "#chat", "reps", "authors"},
		},
		{
			name: "chain reps revokes",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.repsCategory(alias, "revokes")
			},
			expected: []string{"freechains", "chain", "#chat", "reps", "revokes"},
		},
		{
			name: "chain reps action",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.repsAction(alias, actID)
			},
			expected: []string{"freechains", "chain", "#chat", "reps", "action", "3519b231c31e34e6afcb25367a87e232792693e6"},
		},
		{
			name: "chain reps author",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.repsAuthor(alias, pubKey)
			},
			expected: []string{"freechains", "chain", "#chat", "reps", "author", string(pubKey)},
		},
		{
			name: "chain reps revoke",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.repsRevoke(alias, actID)
			},
			expected: []string{"freechains", "chain", "#chat", "reps", "revoke", "3519b231c31e34e6afcb25367a87e232792693e6"},
		},
		{
			name: "chain sync recv",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.sync(alias, "recv", "localhost:8330")
			},
			expected: []string{"freechains", "chain", "#chat", "sync", "recv", "localhost:8330"},
		},
		{
			name: "chain sync send",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.sync(alias, "send", "localhost:8330")
			},
			expected: []string{"freechains", "chain", "#chat", "sync", "send", "localhost:8330"},
		},
		{
			name: "chain abandon without keep",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.abandon(alias, actID, false)
			},
			expected: []string{"freechains", "chain", "#chat", "abandon", "3519b231c31e34e6afcb25367a87e232792693e6"},
		},
		{
			name: "chain abandon with keep",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.abandon(alias, actID, true)
			},
			expected: []string{"freechains", "chain", "#chat", "abandon", "3519b231c31e34e6afcb25367a87e232792693e6", "--keep"},
		},
		{
			name: "chain sweep",
			cfg:  Config{Binary: "freechains"},
			buildFn: func(b *argsBuilder) ([]string, error) {
				return b.sweep(alias)
			},
			expected: []string{"freechains", "chain", "#chat", "sweep"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := newArgsBuilder(tt.cfg.Binary, tt.cfg.Root, tt.cfg.Now)
			actual, err := tt.buildFn(builder)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("slice mismatch:\ngot:  %#v\nwant: %#v", actual, tt.expected)
			}
		})
	}
}
