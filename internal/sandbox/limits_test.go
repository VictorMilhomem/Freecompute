package sandbox

import (
	"errors"
	"testing"
	"time"
)

func TestLimits(t *testing.T) {
	defaults := Limits{
		CPUs:           1.0,
		MemoryBytes:    512 * 1024 * 1024,
		Timeout:        30 * time.Second,
		PidsLimit:      100,
		TmpfsBytes:     64 * 1024 * 1024,
		MaxOutputBytes: 10 * 1024 * 1024,
		MaxOutputFiles: 50,
	}

	hard := Limits{
		CPUs:           2.0,
		MemoryBytes:    1024 * 1024 * 1024,
		Timeout:        60 * time.Second,
		PidsLimit:      200,
		TmpfsBytes:     128 * 1024 * 1024,
		MaxOutputBytes: 50 * 1024 * 1024,
		MaxOutputFiles: 100,
	}

	t.Run("zero values populated from defaults", func(t *testing.T) {
		empty := Limits{}
		applied := empty.ApplyDefaults(defaults)

		if applied.CPUs != defaults.CPUs {
			t.Errorf("CPUs: got %v, want %v", applied.CPUs, defaults.CPUs)
		}
		if applied.MemoryBytes != defaults.MemoryBytes {
			t.Errorf("MemoryBytes: got %v, want %v", applied.MemoryBytes, defaults.MemoryBytes)
		}
		if applied.Timeout != defaults.Timeout {
			t.Errorf("Timeout: got %v, want %v", applied.Timeout, defaults.Timeout)
		}
		if applied.PidsLimit != defaults.PidsLimit {
			t.Errorf("PidsLimit: got %v, want %v", applied.PidsLimit, defaults.PidsLimit)
		}
		if applied.TmpfsBytes != defaults.TmpfsBytes {
			t.Errorf("TmpfsBytes: got %v, want %v", applied.TmpfsBytes, defaults.TmpfsBytes)
		}
		if applied.MaxOutputBytes != defaults.MaxOutputBytes {
			t.Errorf("MaxOutputBytes: got %v, want %v", applied.MaxOutputBytes, defaults.MaxOutputBytes)
		}
		if applied.MaxOutputFiles != defaults.MaxOutputFiles {
			t.Errorf("MaxOutputFiles: got %v, want %v", applied.MaxOutputFiles, defaults.MaxOutputFiles)
		}
	})

	t.Run("specified limits preserved", func(t *testing.T) {
		custom := Limits{
			CPUs:        1.5,
			MemoryBytes: 768 * 1024 * 1024,
		}
		applied := custom.ApplyDefaults(defaults)

		if applied.CPUs != 1.5 {
			t.Errorf("CPUs got %v, want 1.5", applied.CPUs)
		}
		if applied.MemoryBytes != 768*1024*1024 {
			t.Errorf("MemoryBytes got %v, want 768MB", applied.MemoryBytes)
		}
		if applied.Timeout != defaults.Timeout {
			t.Errorf("Timeout got %v, want %v", applied.Timeout, defaults.Timeout)
		}
	})

	t.Run("limits above ceilings rejected with ErrLimitsExceeded", func(t *testing.T) {
		cases := []struct {
			name   string
			limits Limits
		}{
			{"CPUs exceeded", Limits{CPUs: 2.5}},
			{"Memory exceeded", Limits{MemoryBytes: 2048 * 1024 * 1024}},
			{"Timeout exceeded", Limits{Timeout: 90 * time.Second}},
			{"PidsLimit exceeded", Limits{PidsLimit: 500}},
			{"TmpfsBytes exceeded", Limits{TmpfsBytes: 256 * 1024 * 1024}},
			{"MaxOutputBytes exceeded", Limits{MaxOutputBytes: 100 * 1024 * 1024}},
			{"MaxOutputFiles exceeded", Limits{MaxOutputFiles: 200}},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.limits.CheckCeilings(hard)
				if err == nil {
					t.Fatalf("expected error for %s, got nil", tc.name)
				}
				if !errors.Is(err, ErrLimitsExceeded) {
					t.Errorf("expected ErrLimitsExceeded, got %v", err)
				}
			})
		}
	})
}
