package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	debugMode := flag.Bool("debug", false, "Run debug scan without TUI")
	flag.Parse()

	if *debugMode {
		runDebug()
		return
	}

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Store program reference so encoding can send progress messages.
	program = p

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
