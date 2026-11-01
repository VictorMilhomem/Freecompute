//go:build faulty

package main

import (
	"fmt"
	"os"

	"github.com/VictorMilhomem/freecompute/internal/cli"
	"github.com/VictorMilhomem/freecompute/internal/faultinject"
	"github.com/VictorMilhomem/freecompute/pkg/freecompute"
)

func main() {
	fmt.Fprintln(os.Stderr, "WARNING: vcnode-faulty is a fault-injection binary that intentionally produces divergent results on purpose")
	realRunner, err := cli.NewDefaultRunner()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vcnode-faulty error: %v\n", err)
		os.Exit(1)
	}
	if err := cli.Run(os.Args[1:], freecompute.WithRunner(faultinject.WrapRunner(realRunner, []byte("999\n")))); err != nil {
		fmt.Fprintf(os.Stderr, "vcnode-faulty error: %v\n", err)
		os.Exit(1)
	}
}
