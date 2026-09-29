package sandbox

import (
	"fmt"
	"time"
)

// Limits specifies resource and output constraints for a container execution.
type Limits struct {
	CPUs           float64
	MemoryBytes    int64
	Timeout        time.Duration
	PidsLimit      int64
	TmpfsBytes     int64
	MaxOutputBytes int64
	MaxOutputFiles int
}

// ApplyDefaults fills in zero-valued limits from defaults.
func (l Limits) ApplyDefaults(defaults Limits) Limits {
	out := l
	if out.CPUs <= 0 {
		out.CPUs = defaults.CPUs
	}
	if out.MemoryBytes <= 0 {
		out.MemoryBytes = defaults.MemoryBytes
	}
	if out.Timeout <= 0 {
		out.Timeout = defaults.Timeout
	}
	if out.PidsLimit <= 0 {
		out.PidsLimit = defaults.PidsLimit
	}
	if out.TmpfsBytes <= 0 {
		out.TmpfsBytes = defaults.TmpfsBytes
	}
	if out.MaxOutputBytes <= 0 {
		out.MaxOutputBytes = defaults.MaxOutputBytes
	}
	if out.MaxOutputFiles <= 0 {
		out.MaxOutputFiles = defaults.MaxOutputFiles
	}
	return out
}

// CheckCeilings verifies that limits do not exceed worker hard ceilings.
// Rejecting instead of silently clamping prevents untrusted specs from
// executing under unexpected constraints.
func (l Limits) CheckCeilings(hard Limits) error {
	if hard.CPUs > 0 && l.CPUs > hard.CPUs {
		return fmt.Errorf("%w: CPUs %.2f exceeds ceiling %.2f", ErrLimitsExceeded, l.CPUs, hard.CPUs)
	}
	if hard.MemoryBytes > 0 && l.MemoryBytes > hard.MemoryBytes {
		return fmt.Errorf("%w: MemoryBytes %d exceeds ceiling %d", ErrLimitsExceeded, l.MemoryBytes, hard.MemoryBytes)
	}
	if hard.Timeout > 0 && l.Timeout > hard.Timeout {
		return fmt.Errorf("%w: Timeout %v exceeds ceiling %v", ErrLimitsExceeded, l.Timeout, hard.Timeout)
	}
	if hard.PidsLimit > 0 && l.PidsLimit > hard.PidsLimit {
		return fmt.Errorf("%w: PidsLimit %d exceeds ceiling %d", ErrLimitsExceeded, l.PidsLimit, hard.PidsLimit)
	}
	if hard.TmpfsBytes > 0 && l.TmpfsBytes > hard.TmpfsBytes {
		return fmt.Errorf("%w: TmpfsBytes %d exceeds ceiling %d", ErrLimitsExceeded, l.TmpfsBytes, hard.TmpfsBytes)
	}
	if hard.MaxOutputBytes > 0 && l.MaxOutputBytes > hard.MaxOutputBytes {
		return fmt.Errorf("%w: MaxOutputBytes %d exceeds ceiling %d", ErrLimitsExceeded, l.MaxOutputBytes, hard.MaxOutputBytes)
	}
	if hard.MaxOutputFiles > 0 && l.MaxOutputFiles > hard.MaxOutputFiles {
		return fmt.Errorf("%w: MaxOutputFiles %d exceeds ceiling %d", ErrLimitsExceeded, l.MaxOutputFiles, hard.MaxOutputFiles)
	}
	return nil
}
