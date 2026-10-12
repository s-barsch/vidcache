package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: vidcache [-headless] [path]\n\npath limits the scan to a folder in the cache, or the folder a file is in.\n\n")
		flag.PrintDefaults()
	}
	headless := flag.Bool("headless", false, "encode every missing size without the TUI, printing plain lines")
	flag.Parse()

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	scope, err := ScanScope(cfg.CachePath, flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if *headless {
		if err := runHeadless(cfg, scope); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	m := newModel(cfg, scope)
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Store program reference so encoding can send progress messages.
	program = p

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

