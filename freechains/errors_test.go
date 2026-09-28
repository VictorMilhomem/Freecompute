package freechains

import (
	"errors"
	"testing"
)

func TestDefaultErrorClassifier(t *testing.T) {
	tests := []struct {
		name        string
		exitCode    int
		stderr      string
		expectedErr error
	}{
		{
			name:        "insufficient reputation",
			exitCode:    1,
			stderr:      "ERROR : chain post : insufficient reputation",
			expectedErr: ErrInsufficientReps,
		},
		{
			name:        "insufficient reps uppercase",
			exitCode:    1,
			stderr:      "ERROR: INSUFFICIENT REPS",
			expectedErr: ErrInsufficientReps,
		},
		{
			name:        "revoked payload",
			exitCode:    1,
			stderr:      "ERROR : chain get : revoked payload",
			expectedErr: ErrPayloadRevoked,
		},
		{
			name:        "payload revoked variation",
			exitCode:    1,
			stderr:      "error: payload revoked by pioneer",
			expectedErr: ErrPayloadRevoked,
		},
		{
			name:        "action not found / unknown post",
			exitCode:    1,
			stderr:      "ERROR : chain get : unknown post",
			expectedErr: ErrActionNotFound,
		},
		{
			name:        "action not found phrase",
			exitCode:    1,
			stderr:      "error: action not found in repository",
			expectedErr: ErrActionNotFound,
		},
		{
			name:        "chain not found",
			exitCode:    1,
			stderr:      "ERROR : chain #test : not found",
			expectedErr: ErrChainNotFound,
		},
		{
			name:        "requires sign or beg",
			exitCode:    1,
			stderr:      "ERROR : chain post : requires --sign or --beg",
			expectedErr: ErrInvalidArgument,
		},
		{
			name:        "invalid number",
			exitCode:    1,
			stderr:      "ERROR : chain revoke : invalid number : expects at least 1000",
			expectedErr: ErrInvalidArgument,
		},
		{
			name:        "invalid action",
			exitCode:    1,
			stderr:      "ERROR : chain abandon : invalid action",
			expectedErr: ErrInvalidArgument,
		},
		{
			name:        "too many arguments",
			exitCode:    1,
			stderr:      "Error: too many arguments",
			expectedErr: ErrInvalidArgument,
		},
		{
			name:        "unclassified failure returns nil",
			exitCode:    1,
			stderr:      "internal segmentation fault or unknown git-daemon error",
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DefaultErrorClassifier(tt.exitCode, tt.stderr)
			if tt.expectedErr == nil {
				if err != nil {
					t.Fatalf("expected nil for unclassified error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %v, got nil", tt.expectedErr)
			}
			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected error wrapping %v, got %v", tt.expectedErr, err)
			}
		})
	}
}

func TestCommandError(t *testing.T) {
	cmdErr := &CommandError{
		Args:     []string{"freechains", "chains", "rem", "#nonexistent"},
		ExitCode: 1,
		Stderr:   "fatal: something went wrong\n",
	}

	str := cmdErr.Error()
	if str == "" {
		t.Fatal("expected non-empty error string")
	}

	var target *CommandError
	if !errors.As(cmdErr, &target) {
		t.Fatal("errors.As should match *CommandError")
	}
	if target.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", target.ExitCode)
	}
}
