package freechains

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrInsufficientReps is returned when an action fails due to lack of reputation.
	ErrInsufficientReps = errors.New("insufficient reputation")

	// ErrPayloadRevoked is returned when attempting to access a payload that has been revoked.
	ErrPayloadRevoked = errors.New("payload revoked")

	// ErrActionNotFound is returned when an action ID does not exist in the chain.
	ErrActionNotFound = errors.New("action not found")

	// ErrChainNotFound is returned when the specified chain alias does not exist.
	ErrChainNotFound = errors.New("chain not found")

	// ErrCommandTimeout is returned when a command execution exceeds its deadline.
	ErrCommandTimeout = errors.New("command timeout")

	// ErrPayloadTooLarge is returned when a payload exceeds the 128 KB limit.
	ErrPayloadTooLarge = errors.New("payload too large")

	// ErrInvalidArgument is returned when input validation fails.
	ErrInvalidArgument = errors.New("invalid argument")
)

// CommandError captures detailed failure information for unclassified command errors.
type CommandError struct {
	Args     []string
	ExitCode int
	Stderr   string
}

func (e *CommandError) Error() string {
	cleanStderr := strings.TrimSpace(e.Stderr)
	if cleanStderr == "" {
		return fmt.Sprintf("command %v failed with exit code %d", e.Args, e.ExitCode)
	}
	return fmt.Sprintf("command %v failed with exit code %d: %s", e.Args, e.ExitCode, cleanStderr)
}

// ErrorClassifier classifies an exit code and stderr text into a typed sentinel error.
// It returns nil if the error cannot be classified, allowing fallback to CommandError.
type ErrorClassifier func(exitCode int, stderr string) error

// ErrorRule maps case-insensitive substrings found in stderr to a sentinel error.
type ErrorRule struct {
	Substrings []string
	Err        error
}

// DefaultErrorRules defines the table of substring patterns used to classify CLI stderr output.
var DefaultErrorRules = []ErrorRule{
	{Substrings: []string{"insufficient", "reputation"}, Err: ErrInsufficientReps},
	{Substrings: []string{"insufficient", "reps"}, Err: ErrInsufficientReps},
	{Substrings: []string{"revoked payload"}, Err: ErrPayloadRevoked},
	{Substrings: []string{"payload revoked"}, Err: ErrPayloadRevoked},
	{Substrings: []string{"unknown post"}, Err: ErrActionNotFound},
	{Substrings: []string{"action", "not found"}, Err: ErrActionNotFound},
	{Substrings: []string{"chain", "not found"}, Err: ErrChainNotFound},
	{Substrings: []string{"requires --sign or --beg"}, Err: ErrInvalidArgument},
	{Substrings: []string{"invalid number"}, Err: ErrInvalidArgument},
	{Substrings: []string{"invalid action"}, Err: ErrInvalidArgument},
	{Substrings: []string{"invalid argument"}, Err: ErrInvalidArgument},
	{Substrings: []string{"too many arguments"}, Err: ErrInvalidArgument},
}

// DefaultErrorClassifier inspects exit code and stderr against DefaultErrorRules.
func DefaultErrorClassifier(exitCode int, stderr string) error {
	lower := strings.ToLower(stderr)
	for _, rule := range DefaultErrorRules {
		matched := true
		for _, substr := range rule.Substrings {
			if !strings.Contains(lower, strings.ToLower(substr)) {
				matched = false
				break
			}
		}
		if matched {
			return fmt.Errorf("%w: %s", rule.Err, strings.TrimSpace(stderr))
		}
	}
	return nil
}
