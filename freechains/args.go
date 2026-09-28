package freechains

import (
	"fmt"
	"strconv"
	"strings"
)

type argsBuilder struct {
	binary string
	root   string
	now    int64
}

func newArgsBuilder(binary, root string, now int64) *argsBuilder {
	if binary == "" {
		binary = "freechains"
	}
	return &argsBuilder{
		binary: binary,
		root:   root,
		now:    now,
	}
}

// build combines the binary, global options, and command arguments.
// Global options are placed immediately after the binary name.
func (b *argsBuilder) build(cmdArgs ...string) []string {
	args := []string{b.binary}
	args = append(args, b.globalOptions()...)
	args = append(args, cmdArgs...)
	return args
}

func (b *argsBuilder) globalOptions() []string {
	var opts []string
	if b.root != "" {
		opts = append(opts, fmt.Sprintf("--root=%s", b.root))
	}
	if b.now > 0 {
		opts = append(opts, fmt.Sprintf("--now=%d", b.now))
	}
	return opts
}

func (b *argsBuilder) daemonStart(port int, hub bool, gitOpts []string) []string {
	cmdArgs := []string{"daemon", "start"}
	if port > 0 {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--port=%d", port))
	}
	if hub {
		cmdArgs = append(cmdArgs, "--hub")
	}
	if len(gitOpts) > 0 {
		cmdArgs = append(cmdArgs, "--")
		cmdArgs = append(cmdArgs, gitOpts...)
	}
	return b.build(cmdArgs...)
}

func (b *argsBuilder) daemonStop() []string {
	return b.build("daemon", "stop")
}

func (b *argsBuilder) chainsDir() []string {
	return b.build("chains", "dir")
}

func (b *argsBuilder) chainsAddInit(alias Alias, pioneers ...PublicKey) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	cmdArgs := []string{"chains", "add", alias.String(), "init"}
	for _, p := range pioneers {
		if err := p.Validate(); err != nil {
			return nil, err
		}
		cmdArgs = append(cmdArgs, fmt.Sprintf("--pioneer=%s", p.String()))
	}
	return b.build(cmdArgs...), nil
}

func (b *argsBuilder) chainsAddClone(alias Alias, url string) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if len(url) == 0 {
		return nil, fmt.Errorf("%w: clone url cannot be empty", ErrInvalidArgument)
	}
	if strings.HasPrefix(url, "-") {
		return nil, fmt.Errorf("%w: clone url cannot start with '-': %q", ErrInvalidArgument, url)
	}
	return b.build("chains", "add", alias.String(), "clone", url), nil
}

func (b *argsBuilder) chainsRem(alias Alias) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	return b.build("chains", "rem", alias.String()), nil
}

func (b *argsBuilder) post(alias Alias, payload Payload, opts PostOptions) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	if opts.Beg && opts.Sign != "" {
		return nil, fmt.Errorf("%w: cannot specify both --sign and --beg", ErrInvalidArgument)
	}
	if !opts.Beg && opts.Sign == "" {
		return nil, fmt.Errorf("%w: must specify either --sign or --beg", ErrInvalidArgument)
	}
	if opts.Sign != "" {
		if err := opts.Sign.Validate(); err != nil {
			return nil, err
		}
	}

	cmdArgs := []string{"chain", alias.String(), "post"}
	switch payload.Kind() {
	case PayloadInline:
		cmdArgs = append(cmdArgs, "inline", payload.Text())
	case PayloadFile:
		cmdArgs = append(cmdArgs, "file", payload.Path())
	}

	if opts.Beg {
		cmdArgs = append(cmdArgs, "--beg")
	} else {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--sign=%s", opts.Sign.String()))
	}

	return b.build(cmdArgs...), nil
}

func (b *argsBuilder) like(alias Alias, amount int64, target Target, opts VoteOptions) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if amount < 0 {
		return nil, fmt.Errorf("%w: vote amount cannot be negative: %d", ErrInvalidArgument, amount)
	}
	if err := target.Validate(); err != nil {
		return nil, err
	}

	cmdArgs := []string{"chain", alias.String(), "like", strconv.FormatInt(amount, 10)}
	if target.Kind() == TargetAction {
		cmdArgs = append(cmdArgs, "action", target.Action().String())
	} else {
		cmdArgs = append(cmdArgs, "author", target.Author().String())
	}

	if opts.Sign != "" {
		if err := opts.Sign.Validate(); err != nil {
			return nil, err
		}
		cmdArgs = append(cmdArgs, fmt.Sprintf("--sign=%s", opts.Sign.String()))
	}
	if opts.Why != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--why=%s", opts.Why))
	}
	if opts.File != "" {
		if strings.HasPrefix(opts.File, "-") {
			return nil, fmt.Errorf("%w: file path cannot start with '-': %q", ErrInvalidArgument, opts.File)
		}
		cmdArgs = append(cmdArgs, fmt.Sprintf("--file=%s", opts.File))
	}

	return b.build(cmdArgs...), nil
}

func (b *argsBuilder) dislike(alias Alias, amount int64, target Target, opts VoteOptions) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if amount < 0 {
		return nil, fmt.Errorf("%w: vote amount cannot be negative: %d", ErrInvalidArgument, amount)
	}
	if err := target.Validate(); err != nil {
		return nil, err
	}

	cmdArgs := []string{"chain", alias.String(), "dislike", strconv.FormatInt(amount, 10)}
	if target.Kind() == TargetAction {
		cmdArgs = append(cmdArgs, "action", target.Action().String())
	} else {
		cmdArgs = append(cmdArgs, "author", target.Author().String())
	}

	if opts.Sign != "" {
		if err := opts.Sign.Validate(); err != nil {
			return nil, err
		}
		cmdArgs = append(cmdArgs, fmt.Sprintf("--sign=%s", opts.Sign.String()))
	}
	if opts.Why != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--why=%s", opts.Why))
	}

	return b.build(cmdArgs...), nil
}

func (b *argsBuilder) revoke(alias Alias, amount int64, id ActionID, opts VoteOptions) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if amount < 0 {
		return nil, fmt.Errorf("%w: revoke amount cannot be negative: %d", ErrInvalidArgument, amount)
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}

	cmdArgs := []string{"chain", alias.String(), "revoke", strconv.FormatInt(amount, 10), id.String()}
	if opts.Sign != "" {
		if err := opts.Sign.Validate(); err != nil {
			return nil, err
		}
		cmdArgs = append(cmdArgs, fmt.Sprintf("--sign=%s", opts.Sign.String()))
	}
	if opts.Why != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--why=%s", opts.Why))
	}

	return b.build(cmdArgs...), nil
}

func (b *argsBuilder) unrevoke(alias Alias, amount int64, id ActionID, opts VoteOptions) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if amount < 0 {
		return nil, fmt.Errorf("%w: unrevoke amount cannot be negative: %d", ErrInvalidArgument, amount)
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}

	cmdArgs := []string{"chain", alias.String(), "unrevoke", strconv.FormatInt(amount, 10), id.String()}
	if opts.Sign != "" {
		if err := opts.Sign.Validate(); err != nil {
			return nil, err
		}
		cmdArgs = append(cmdArgs, fmt.Sprintf("--sign=%s", opts.Sign.String()))
	}
	if opts.Why != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--why=%s", opts.Why))
	}
	if opts.File != "" {
		if strings.HasPrefix(opts.File, "-") {
			return nil, fmt.Errorf("%w: file path cannot start with '-': %q", ErrInvalidArgument, opts.File)
		}
		cmdArgs = append(cmdArgs, fmt.Sprintf("--file=%s", opts.File))
	}

	return b.build(cmdArgs...), nil
}

func (b *argsBuilder) list(alias Alias, mode string) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	return b.build("chain", alias.String(), "list", mode), nil
}

func (b *argsBuilder) get(alias Alias, target string, id ActionID) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}
	return b.build("chain", alias.String(), "get", target, id.String()), nil
}

func (b *argsBuilder) repsCategory(alias Alias, category string) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	return b.build("chain", alias.String(), "reps", category), nil
}

func (b *argsBuilder) repsAction(alias Alias, id ActionID) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}
	return b.build("chain", alias.String(), "reps", "action", id.String()), nil
}

func (b *argsBuilder) repsAuthor(alias Alias, pub PublicKey) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	return b.build("chain", alias.String(), "reps", "author", pub.String()), nil
}

func (b *argsBuilder) repsRevoke(alias Alias, id ActionID) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}
	return b.build("chain", alias.String(), "reps", "revoke", id.String()), nil
}

func (b *argsBuilder) sync(alias Alias, direction, remote string) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if len(remote) == 0 {
		return nil, fmt.Errorf("%w: remote address cannot be empty", ErrInvalidArgument)
	}
	if strings.HasPrefix(remote, "-") {
		return nil, fmt.Errorf("%w: remote address cannot start with '-': %q", ErrInvalidArgument, remote)
	}
	return b.build("chain", alias.String(), "sync", direction, remote), nil
}

func (b *argsBuilder) abandon(alias Alias, id ActionID, keep bool) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}
	cmdArgs := []string{"chain", alias.String(), "abandon", id.String()}
	if keep {
		cmdArgs = append(cmdArgs, "--keep")
	}
	return b.build(cmdArgs...), nil
}

func (b *argsBuilder) sweep(alias Alias) ([]string, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	return b.build("chain", alias.String(), "sweep"), nil
}
