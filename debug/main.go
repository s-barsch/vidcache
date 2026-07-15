package main

import (
	"fmt"
	"os"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Starting scan...")
	result, err := ScanVideos(cfg.CachePath, func(msg string) {
		// print progress
		fmt.Println(msg)
	})
	if err != nil {
		fmt.Printf("Scan error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nScan complete. Found %d videos.\n", len(result.Videos))
	fmt.Printf("OK: %d, Needs Rename: %d, Needs Cache: %d\n", result.OKCount, result.RenameCount, result.CacheCount)
	if len(result.Errors) > 0 {
		fmt.Println("Errors:")
		for _, e := range result.Errors {
			fmt.Println("-", e)
		}
	}

	for _, v := range result.Videos {
		fmt.Printf("- %s (Res: %s, %dx%d, Status: %s)\n", v.Filename, v.ActualRes.Tag, v.Width, v.Height, v.Status)
	}
}
