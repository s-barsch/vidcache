package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// runHeadless encodes every missing size without asking and prints one plain
// line per step. Renames are reported but left for the TUI.
func runHeadless(cfg Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root, err := filepath.EvalSymlinks(cfg.CachePath)
	if err != nil {
		return fmt.Errorf("resolving cache path %s: %w", cfg.CachePath, err)
	}
	rel := func(path string) string {
		if r, err := filepath.Rel(root, path); err == nil {
			return r
		}
		return path
	}

	fmt.Printf("scanning %s\n", root)
	result, err := ScanVideos(root, nil)
	if err != nil {
		return err
	}
	for _, e := range result.Errors {
		fmt.Printf("error: %s\n", e)
	}

	var jobs []encodeJob
	for _, v := range result.Videos {
		if v.NeedsRename {
			fmt.Printf("needs rename: %s → %s\n", rel(v.Path), v.CorrectFilename())
		}
		for _, res := range v.MissingSizes {
			jobs = append(jobs, encodeJob{video: v, res: res})
		}
	}
	fmt.Printf("%d videos, %d sizes to encode\n", len(result.Videos), len(jobs))

	failed := len(result.Errors)
	for i, job := range jobs {
		fmt.Printf("[%d/%d] %s → %s\n", i+1, len(jobs), rel(job.video.Path), job.res.Tag)
		start := time.Now()

		progressCh := make(chan EncodeProgress, 10)
		errCh := make(chan error, 1)
		go func() {
			errCh <- Transcode(ctx, job.video.Path, job.video.SizedPath(job.res),
				job.res.Height, job.video.IsPortrait, job.video.Duration, progressCh)
		}()
		quarter := 0
		for p := range progressCh {
			if q := int(p.Percent * 4); q > quarter && q < 4 {
				quarter = q
				fmt.Printf("  %d%%\n", q*25)
			}
		}

		if err := <-errCh; err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("stopped during %s", rel(job.video.SizedPath(job.res)))
			}
			failed++
			fmt.Printf("  error: %v\n", err)
			continue
		}
		fmt.Printf("  done in %s\n", time.Since(start).Round(time.Second))
	}

	if failed > 0 {
		return fmt.Errorf("%d failed", failed)
	}
	return nil
}
