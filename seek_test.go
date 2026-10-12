package main

import (
	"strings"
	"testing"
)

func TestSeekGridKeepsShortVideosAtOneFramePerSecond(t *testing.T) {
	g := newSeekGrid(1920, 1080, 30)

	if g.Interval != 1 || g.Count != 30 {
		t.Fatalf("got interval %v count %d, want 1 and 30", g.Interval, g.Count)
	}
	if g.Columns() != 10 || g.Rows() != 3 {
		t.Fatalf("got %dx%d grid, want 10x3", g.Columns(), g.Rows())
	}
}

func TestSeekGridSpreadsLongVideosOverAtMostAHundredFrames(t *testing.T) {
	g := newSeekGrid(1920, 1080, 600)

	if g.Interval != 6 || g.Count != 100 {
		t.Fatalf("got interval %v count %d, want 6 and 100", g.Interval, g.Count)
	}
}

func TestSeekGridSizesTilesByTheLongSide(t *testing.T) {
	landscape := newSeekGrid(1920, 1080, 10)
	if landscape.TileW != 240 || landscape.TileH != 134 {
		t.Fatalf("landscape tile %dx%d, want 240x134", landscape.TileW, landscape.TileH)
	}

	portrait := newSeekGrid(1080, 1920, 10)
	if portrait.TileW != 134 || portrait.TileH != 240 {
		t.Fatalf("portrait tile %dx%d, want 134x240", portrait.TileW, portrait.TileH)
	}
}

func TestSeekGridWithFewFramesUsesOneShortRow(t *testing.T) {
	g := newSeekGrid(1920, 1080, 4.2)

	if g.Count != 5 || g.Columns() != 5 || g.Rows() != 1 {
		t.Fatalf("got count %d grid %dx%d, want 5 in 5x1", g.Count, g.Columns(), g.Rows())
	}
}

func TestSeekTrackPointsEachIntervalAtItsTile(t *testing.T) {
	g := newSeekGrid(1920, 1080, 11.5)
	track := g.Track("200831_223743.jpg", 11.5)

	if !strings.HasPrefix(track, "WEBVTT\n") {
		t.Fatalf("track does not start with WEBVTT: %q", track)
	}

	for _, want := range []string{
		"00:00:00.000 --> 00:00:01.000\n200831_223743.jpg#xywh=0,0,240,134\n",
		"00:00:09.000 --> 00:00:10.000\n200831_223743.jpg#xywh=2160,0,240,134\n",
		"00:00:10.000 --> 00:00:11.000\n200831_223743.jpg#xywh=0,134,240,134\n",
		"00:00:11.000 --> 00:00:11.500\n200831_223743.jpg#xywh=240,134,240,134\n",
	} {
		if !strings.Contains(track, want) {
			t.Errorf("track lacks cue %q", want)
		}
	}
}

func TestVTTTimeCarriesHours(t *testing.T) {
	if got := vttTime(3723.25); got != "01:02:03.250" {
		t.Fatalf("got %s", got)
	}
}
