package freechains

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeMockScript(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "mock_freechains")
	if err := os.WriteFile(scriptPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}
	return scriptPath
}

func TestDaemonLifecycle(t *testing.T) {
	t.Run("start and stop success", func(t *testing.T) {
		pidFile := filepath.Join(t.TempDir(), "daemon.pid")
		scriptContent := fmt.Sprintf(`#!/bin/sh
case "$1" in
    daemon)
        case "$2" in
            start)
                echo $$ > %s
                echo "Serving on port 8330..."
                while true; do sleep 1; done
                ;;
            stop)
                if [ -f %s ]; then
                    kill $(cat %s) 2>/dev/null
                fi
                exit 0
                ;;
        esac
        ;;
esac
exit 0
`, pidFile, pidFile, pidFile)

		scriptPath := writeMockScript(t, scriptContent)
		runner := NewExecRunner()

		cfg := DaemonConfig{
			Binary: scriptPath,
		}

		daemon, err := NewDaemon(runner, cfg)
		if err != nil {
			t.Fatalf("failed to create daemon: %v", err)
		}

		startCtx, startCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer startCancel()

		if err := daemon.Start(startCtx); err != nil {
			t.Fatalf("failed to start daemon: %v", err)
		}

		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopCancel()

		if err := daemon.Stop(stopCtx); err != nil {
			t.Fatalf("failed to stop daemon: %v", err)
		}
	})

	t.Run("early exit reported during startup", func(t *testing.T) {
		scriptContent := `#!/bin/sh
>&2 echo "failed to bind port"
exit 1
`
		scriptPath := writeMockScript(t, scriptContent)
		runner := NewExecRunner()

		cfg := DaemonConfig{
			Binary: scriptPath,
		}

		daemon, err := NewDaemon(runner, cfg)
		if err != nil {
			t.Fatalf("failed to create daemon: %v", err)
		}

		startCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err = daemon.Start(startCtx)
		if err == nil {
			t.Fatal("expected error on premature daemon exit, got nil")
		}
		if !strings.Contains(err.Error(), "failed to bind port") {
			t.Errorf("expected stderr in error message, got %v", err)
		}
	})

	t.Run("start timeout honors context", func(t *testing.T) {
		scriptContent := `#!/bin/sh
while true; do sleep 1; done
`
		scriptPath := writeMockScript(t, scriptContent)
		runner := NewExecRunner()

		cfg := DaemonConfig{
			Binary: scriptPath,
		}

		daemon, err := NewDaemon(runner, cfg)
		if err != nil {
			t.Fatalf("failed to create daemon: %v", err)
		}

		startCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err = daemon.Start(startCtx)
		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}
	})
}
