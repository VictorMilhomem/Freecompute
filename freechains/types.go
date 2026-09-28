package freechains

import (
	"fmt"
	"os"
	"strings"
)

// MaxPayloadSize defines the maximum allowed post payload size (128 KB).
const MaxPayloadSize = 128 * 1024

// Alias represents a chain alias. It must start with '#' and have no whitespace.
type Alias string

func (a Alias) String() string {
	return string(a)
}

// Validate ensures the alias conforms to Freechains rules.
func (a Alias) Validate() error {
	s := string(a)
	if len(s) == 0 {
		return fmt.Errorf("%w: alias cannot be empty", ErrInvalidArgument)
	}
	if !strings.HasPrefix(s, "#") {
		return fmt.Errorf("%w: alias must start with '#': %q", ErrInvalidArgument, s)
	}
	if len(s) == 1 {
		return fmt.Errorf("%w: alias cannot be just '#'", ErrInvalidArgument)
	}
	if strings.ContainsAny(s, " \t\n\r") {
		return fmt.Errorf("%w: alias cannot contain whitespace: %q", ErrInvalidArgument, s)
	}
	return nil
}

// ActionID represents a 40-character hex action identifier.
type ActionID string

func (id ActionID) String() string {
	return string(id)
}

// Validate ensures the action ID is a non-empty, valid 40-character hex string.
func (id ActionID) Validate() error {
	s := string(id)
	if len(s) == 0 {
		return fmt.Errorf("%w: action id cannot be empty", ErrInvalidArgument)
	}
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("%w: action id cannot start with '-': %q", ErrInvalidArgument, s)
	}
	if len(s) != 40 {
		return fmt.Errorf("%w: action id must be 40 hex characters: %q", ErrInvalidArgument, s)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return fmt.Errorf("%w: action id must be hex characters: %q", ErrInvalidArgument, s)
		}
	}
	return nil
}

// ChainID represents the unique identifier of a chain returned by chains add.
type ChainID string

func (c ChainID) String() string {
	return string(c)
}

// Validate ensures the chain ID is valid.
func (c ChainID) Validate() error {
	s := string(c)
	if len(s) == 0 {
		return fmt.Errorf("%w: chain id cannot be empty", ErrInvalidArgument)
	}
	if !strings.HasPrefix(s, "#") {
		return fmt.Errorf("%w: chain id must start with '#': %q", ErrInvalidArgument, s)
	}
	if strings.ContainsAny(s, " \t\n\r") {
		return fmt.Errorf("%w: chain id cannot contain whitespace: %q", ErrInvalidArgument, s)
	}
	return nil
}

// PublicKey represents an SSH public key string or a path to a public key file.
type PublicKey string

func (pk PublicKey) String() string {
	return string(pk)
}

// Validate ensures the public key is non-empty and does not begin with '-'.
func (pk PublicKey) Validate() error {
	s := string(pk)
	if len(s) == 0 {
		return fmt.Errorf("%w: public key cannot be empty", ErrInvalidArgument)
	}
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("%w: public key cannot start with '-': %q", ErrInvalidArgument, s)
	}
	return nil
}

// PrivateKeyPath represents a filesystem path to a private key file.
type PrivateKeyPath string

func (pk PrivateKeyPath) String() string {
	return string(pk)
}

// Validate ensures the private key path is non-empty and does not begin with '-'.
func (pk PrivateKeyPath) Validate() error {
	s := string(pk)
	if len(s) == 0 {
		return fmt.Errorf("%w: private key path cannot be empty", ErrInvalidArgument)
	}
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("%w: private key path cannot start with '-': %q", ErrInvalidArgument, s)
	}
	return nil
}

// PayloadType represents whether a payload is inline text or a file.
type PayloadType int

const (
	PayloadInline PayloadType = iota
	PayloadFile
)

// Payload is a sum-like type carrying inline text or a file path for posts.
type Payload struct {
	kind PayloadType
	text string
	path string
}

// FilePayload constructs a Payload pointing to a file on disk.
func FilePayload(path string) Payload {
	return Payload{kind: PayloadFile, path: path}
}

// InlinePayload constructs a Payload with inline text content.
func InlinePayload(text string) Payload {
	return Payload{kind: PayloadInline, text: text}
}

// Kind returns whether the payload is file-based or inline.
func (p Payload) Kind() PayloadType {
	return p.kind
}

// Path returns the file path if this is a file payload.
func (p Payload) Path() string {
	return p.path
}

// Text returns the inline text if this is an inline payload.
func (p Payload) Text() string {
	return p.text
}

// Validate ensures the payload conforms to size and security constraints.
func (p Payload) Validate() error {
	switch p.kind {
	case PayloadInline:
		if strings.HasPrefix(p.text, "-") {
			return fmt.Errorf("%w: inline payload text cannot start with '-'", ErrInvalidArgument)
		}
		if len([]byte(p.text)) > MaxPayloadSize {
			return fmt.Errorf("%w: inline payload size %d exceeds %d bytes", ErrPayloadTooLarge, len([]byte(p.text)), MaxPayloadSize)
		}
		return nil
	case PayloadFile:
		if len(p.path) == 0 {
			return fmt.Errorf("%w: file payload path cannot be empty", ErrInvalidArgument)
		}
		if strings.HasPrefix(p.path, "-") {
			return fmt.Errorf("%w: file payload path cannot start with '-': %q", ErrInvalidArgument, p.path)
		}
		info, err := os.Stat(p.path)
		if err == nil {
			if info.Size() > MaxPayloadSize {
				return fmt.Errorf("%w: file size %d exceeds %d bytes", ErrPayloadTooLarge, info.Size(), MaxPayloadSize)
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: invalid payload type", ErrInvalidArgument)
	}
}

// PostOptions controls signing and beg behavior when posting to a chain.
type PostOptions struct {
	Sign PrivateKeyPath
	Beg  bool
}

// TargetType differentiates vote targets between actions and authors.
type TargetType int

const (
	TargetAction TargetType = iota
	TargetAuthor
)

// Target specifies an action or author to vote on.
type Target struct {
	kind   TargetType
	action ActionID
	author PublicKey
}

// ActionTarget creates a Target pointing to an action ID.
func ActionTarget(id ActionID) Target {
	return Target{kind: TargetAction, action: id}
}

// AuthorTarget creates a Target pointing to an author public key.
func AuthorTarget(pub PublicKey) Target {
	return Target{kind: TargetAuthor, author: pub}
}

// Kind returns whether the target is an action or author.
func (t Target) Kind() TargetType {
	return t.kind
}

// Action returns the target ActionID.
func (t Target) Action() ActionID {
	return t.action
}

// Author returns the target PublicKey.
func (t Target) Author() PublicKey {
	return t.author
}

// Validate ensures the target has a valid action ID or public key.
func (t Target) Validate() error {
	switch t.kind {
	case TargetAction:
		return t.action.Validate()
	case TargetAuthor:
		return t.author.Validate()
	default:
		return fmt.Errorf("%w: unknown target type", ErrInvalidArgument)
	}
}

// VoteOptions controls signing, reasons, and original payloads for votes.
type VoteOptions struct {
	Sign PrivateKeyPath
	Why  string
	File string
}

// OrderEntry represents an action entry in consensus order.
type OrderEntry struct {
	ID      ActionID
	Revoked bool
}

// ActionKind represents the action kind in action metadata.
type ActionKind string

const (
	ActionPost   ActionKind = "post"
	ActionLike   ActionKind = "like"
	ActionRevoke ActionKind = "revoke"
)

// Metadata contains the metadata fields of an action.
type Metadata struct {
	ID   ActionID
	Kind ActionKind
	// Time is the author's local clock and is not trustworthy. Expose it, but never use it for ordering inside the library.
	Time         int64
	Amount       *int64
	TargetAction *ActionID
	TargetAuthor *PublicKey
	Blob         string
	Signer       *PublicKey
	Backs        []ActionID
}

// ActionRep holds an action ID and its current reputation.
type ActionRep struct {
	ID   ActionID
	Reps int64
}

// AuthorRep holds an author public key and their current reputation.
type AuthorRep struct {
	Author PublicKey
	Reps   int64
}

// RevokeRep holds an action ID and its author/others revoke reputation.
type RevokeRep struct {
	Target ActionID
	Author int64
	Others int64
}

// RevokeReps holds the author and others revoke reputation for an action.
type RevokeReps struct {
	Author int64
	Others int64
}
