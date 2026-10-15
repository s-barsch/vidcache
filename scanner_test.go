package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteMetaFile(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "clip-1080.mp4")
	meta := filepath.Join(dir, "sizes", "clip"+durationSuffix)
	if err := os.WriteFile(video, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	read := func() string {
		b, err := os.ReadFile(meta)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	writeMetaFile(video, meta, "197.162")
	if got := read(); got != "197.162" {
		t.Fatalf("missing file: got %q", got)
	}

	writeMetaFile(video, meta, "1")
	if got := read(); got != "197.162" {
		t.Fatalf("fresh file was rewritten: got %q", got)
	}

	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(meta, old, old); err != nil {
		t.Fatal(err)
	}
	writeMetaFile(video, meta, "42.5")
	if got := read(); got != "42.5" {
		t.Fatalf("stale file was kept: got %q", got)
	}
}

func TestIsMetaFile(t *testing.T) {
	for name, want := range map[string]bool{
		"clip.dims.txt":     true,
		"clip.duration.txt": true,
		"clip-720.mp4":      false,
		"clip.txt":          false,
	} {
		if got := isMetaFile(name); got != want {
			t.Errorf("isMetaFile(%q) = %v, want %v", name, got, want)
		}
	}
}
