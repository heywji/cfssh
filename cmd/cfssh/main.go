package main

import (
	"fmt"
	"os"

	"cfssh/internal/orchestrator"
	"cfssh/internal/supervisor"
	"cfssh/internal/ui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "helper" {
		supervisor.RunHelper(os.Args[2:])
		return
	}
	if err := supervisor.PrepareProcess(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ui.Run(&orchestrator.Orchestrator{})
}
