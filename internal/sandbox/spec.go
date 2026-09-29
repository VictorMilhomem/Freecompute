package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
)

// ImagePolicy decides whether an image is permitted to execute.
type ImagePolicy interface {
	Allow(ref ImageRef) error
}

// Spec defines the configuration for a container execution.
type Spec struct {
	Image     ImageRef
	Cmd       []string
	InputDir  string
	OutputDir string
	Limits    Limits
	Labels    map[string]string
}

// Validate verifies that the spec conforms to security and resource requirements.
func (s *Spec) Validate(policy ImagePolicy, defaultLimits, hardLimits Limits) error {
	if _, err := ParseImageRef(s.Image.String()); err != nil {
		return err
	}

	if policy != nil {
		if err := policy.Allow(s.Image); err != nil {
			return fmt.Errorf("%w: %v", ErrImageNotAllowed, err)
		}
	}

	if len(s.Cmd) == 0 {
		return fmt.Errorf("%w: Cmd must not be empty", ErrInvalidArgument)
	}

	if !filepath.IsAbs(s.InputDir) {
		return fmt.Errorf("%w: InputDir %q must be absolute", ErrInvalidArgument, s.InputDir)
	}
	inInfo, err := os.Stat(s.InputDir)
	if err != nil {
		return fmt.Errorf("%w: InputDir does not exist: %v", ErrInvalidArgument, err)
	}
	if !inInfo.IsDir() {
		return fmt.Errorf("%w: InputDir %q is not a directory", ErrInvalidArgument, s.InputDir)
	}

	if !filepath.IsAbs(s.OutputDir) {
		return fmt.Errorf("%w: OutputDir %q must be absolute", ErrInvalidArgument, s.OutputDir)
	}
	outInfo, err := os.Stat(s.OutputDir)
	if err != nil {
		return fmt.Errorf("%w: OutputDir does not exist: %v", ErrInvalidArgument, err)
	}
	if !outInfo.IsDir() {
		return fmt.Errorf("%w: OutputDir %q is not a directory", ErrInvalidArgument, s.OutputDir)
	}

	s.Limits = s.Limits.ApplyDefaults(defaultLimits)
	if err := s.Limits.CheckCeilings(hardLimits); err != nil {
		return err
	}

	return nil
}
