package main

import (
	"fmt"
	"os"

	"github.com/VictorMilhomem/freecompute/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "vcnode error: %v\n", err)
		os.Exit(1)
	}
}
