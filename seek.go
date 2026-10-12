package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
)

const (
	seekDirName   = "seek"
	seekTag       = "seek"
	seekMaxFrames = 100
	seekColumns   = 10
	seekTileLong  = 240
)

// seekGrid is how a video's preview frames are laid out on its sprite sheet:
// one frame per interval, row by row, seekColumns to a row.
type seekGrid struct {
	Interval float64 // seconds each frame stands for
	Count    int     // number of frames
	TileW    int
	TileH    int
}

// newSeekGrid spaces at most seekMaxFrames frames over the video, never closer
// than a second apart, each seekTileLong pixels on its long side.
func newSeekGrid(width, height int, duration float64) seekGrid {
	interval := math.Max(1, math.Ceil(duration/seekMaxFrames))
	count := max(1, int(math.Ceil(duration/interval)))

	tileW, tileH := seekTileLong, seekTileLong
	if width > 0 && height > 0 {
		if width >= height {
			tileH = evenFloor(seekTileLong * height / width)
		} else {
			tileW = evenFloor(seekTileLong * width / height)
		}
	}

	return seekGrid{Interval: interval, Count: count, TileW: tileW, TileH: tileH}
}

func evenFloor(n int) int {
	return max(2, n&^1)
}

func (g seekGrid) Columns() int {
	return min(g.Count, seekColumns)
}

func (g seekGrid) Rows() int {
	return (g.Count + seekColumns - 1) / seekColumns
}

// Track renders the WebVTT track that points each interval at its tile on
// the sprite, named as a file beside the track.
func (g seekGrid) Track(sprite string, duration float64) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n")

	for i := range g.Count {
		start := float64(i) * g.Interval
		end := math.Min(start+g.Interval, duration)
		if end <= start {
			end = start + g.Interval
		}

		x := (i % seekColumns) * g.TileW
		y := (i / seekColumns) * g.TileH

		fmt.Fprintf(&b, "\n%s --> %s\n%s#xywh=%d,%d,%d,%d\n",
			vttTime(start), vttTime(end), sprite, x, y, g.TileW, g.TileH)
	}

	return b.String()
}

func vttTime(seconds float64) string {
	ms := int(math.Round(seconds * 1000))
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3_600_000, ms/60_000%60, ms/1000%60, ms%1000)
}

// GenerateSeek writes the seek preview of a video: a sprite sheet of frames,
// each taken from the middle of its interval, and the track that maps time
// to tiles. The last frame is held for one more interval, so a final interval
// whose middle lies past the end still gets a frame. The sprite is written
// first, so a run stopped halfway leaves no track behind and the preview
// counts as missing.
func GenerateSeek(ctx context.Context, v *VideoFile, progressCh chan<- EncodeProgress) error {
	defer close(progressCh)

	if err := os.MkdirAll(v.SeekDir(), 0o755); err != nil {
		return fmt.Errorf("creating seek dir: %w", err)
	}

	grid := newSeekGrid(v.Width, v.Height, v.Duration)
	offset := math.Min(grid.Interval, v.Duration) / 2

	sprite := v.SeekSpritePath()
	tmpSprite := strings.TrimSuffix(sprite, ".jpg") + ".tmp.jpg"
	defer os.Remove(tmpSprite)

	args := []string{
		"-ss", fmt.Sprintf("%.3f", offset),
		"-i", v.SmallestSource(),
		"-vf", fmt.Sprintf("tpad=stop_mode=clone:stop_duration=%g,fps=1/%g,scale=%d:%d,tile=%dx%d",
			grid.Interval, grid.Interval, grid.TileW, grid.TileH, grid.Columns(), grid.Rows()),
		"-frames:v", "1",
		"-update", "1",
		"-q:v", "5",
		"-an",
		"-y",
		tmpSprite,
	}

	if err := runFFmpeg(ctx, args, v.Duration, progressCh); err != nil {
		return err
	}

	if err := os.Rename(tmpSprite, sprite); err != nil {
		return fmt.Errorf("moving sprite: %w", err)
	}

	track := v.SeekTrackPath()
	tmpTrack := strings.TrimSuffix(track, ".vtt") + ".tmp.vtt"
	defer os.Remove(tmpTrack)

	if err := os.WriteFile(tmpTrack, []byte(grid.Track(v.SeekSpriteFilename(), v.Duration)), 0o644); err != nil {
		return fmt.Errorf("writing track: %w", err)
	}

	if err := os.Rename(tmpTrack, track); err != nil {
		return fmt.Errorf("moving track: %w", err)
	}

	return nil
}
