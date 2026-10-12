package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	headless := flag.Bool("headless", false, "encode every missing size without the TUI, printing plain lines")
	flag.Parse()

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if *headless {
		if err := runHeadless(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
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

