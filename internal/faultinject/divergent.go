//go:build faulty

package faultinject

import (
	"context"
	"os"
	"path/filepath"

	"github.com/VictorMilhomem/freecompute/internal/sandbox"
)

type divergentRunner struct {
	inner   sandbox.Runner
	replace []byte
}

// WrapRunner decorates a sandbox.Runner so that after the real container finishes,
// result.txt in the output directory is overwritten with replace to force a divergent digest.
func WrapRunner(r sandbox.Runner, replace []byte) sandbox.Runner {
	return &divergentRunner{
		inner:   r,
		replace: append([]byte(nil), replace...),
	}
}

func (d *divergentRunner) Run(ctx context.Context, spec sandbox.Spec) (sandbox.Result, error) {
	res, err := d.inner.Run(ctx, spec)
	if err != nil {
		return res, err
	}
	outFile := filepath.Join(spec.OutputDir, "result.txt")
	// Rootless Podman creates files owned by container UID 65534 (mapped subuid);
	// removing before recreating avoids EACCES when overwriting from the host user.
	_ = os.Remove(outFile)
	if err := os.WriteFile(outFile, d.replace, 0o644); err != nil {
		return res, err
	}
	return res, nil
}

func (d *divergentRunner) EnsureImage(ctx context.Context, ref sandbox.ImageRef) error {
	return d.inner.EnsureImage(ctx, ref)
}

func (d *divergentRunner) Cleanup(ctx context.Context) error {
	return d.inner.Cleanup(ctx)
}

func (d *divergentRunner) Ping(ctx context.Context) error {
	return d.inner.Ping(ctx)
}
