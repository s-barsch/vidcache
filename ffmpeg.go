package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// probeResult holds the parsed ffprobe output.
type probeResult struct {
	Width    int
	Height   int
	Duration float64
}

// Probe calls ffprobe to get video dimensions and duration.
func Probe(path string) (probeResult, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_entries", "stream=width,height",
		"-show_entries", "format=duration",
		"-select_streams", "v:0",
		path,
	)

	out, err := cmd.Output()
	if err != nil {
		return probeResult{}, fmt.Errorf("ffprobe %s: %w", path, err)
	}

	var data struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}

	if err := json.Unmarshal(out, &data); err != nil {
		return probeResult{}, fmt.Errorf("parsing ffprobe output for %s: %w", path, err)
	}

	if len(data.Streams) == 0 {
		return probeResult{}, fmt.Errorf("no video stream found in %s", path)
	}

	dur, _ := strconv.ParseFloat(data.Format.Duration, 64)

	return probeResult{
		Width:    data.Streams[0].Width,
		Height:   data.Streams[0].Height,
		Duration: dur,
	}, nil
}

// EncodeProgress is sent on the progress channel during transcoding.
type EncodeProgress struct {
	Frame   int
	Time    float64 // seconds encoded so far
	Speed   string  // e.g. "1.2x"
	Percent float64 // 0.0 to 1.0
}

// Transcode converts a video to the target resolution, preserving orientation.
// Progress updates are sent on progressCh. The channel is closed when done.
func Transcode(ctx context.Context, src, dst string, targetHeight int, isPortrait bool, duration float64, progressCh chan<- EncodeProgress) error {
	defer close(progressCh)

	// Ensure the destination directory exists.
	if err := os.MkdirAll(strings.TrimSuffix(dst, "/"+lastPathComponent(dst)), 0o755); err != nil {
		return fmt.Errorf("creating sizes dir: %w", err)
	}

	// Build the scale filter based on orientation.
	// -2 ensures the other dimension is divisible by 2 (required by most codecs).
	var scaleFilter string
	if isPortrait {
		// Portrait: the resolution tag refers to the width.
		// e.g. -720 on a 1080×1920 portrait → 720×1280.
		scaleFilter = fmt.Sprintf("scale=%d:-2", targetHeight)
	} else {
		// Landscape: the resolution tag refers to the height.
		// e.g. -1080 on a 3840×2160 landscape → 1920×1080.
		scaleFilter = fmt.Sprintf("scale=-2:%d", targetHeight)
	}

	// Use a temp file to avoid partial outputs.
	tmpDst := dst + ".tmp.mp4"
	defer os.Remove(tmpDst)

	args := []string{
		"-i", src,
		"-vf", scaleFilter,
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "23",
		"-c:a", "copy",
		"-movflags", "+faststart",
		"-progress", "pipe:1",
		"-nostats",
		"-y",
		tmpDst,
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = nil // suppress ffmpeg stderr noise

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting ffmpeg: %w", err)
	}

	// Parse ffmpeg progress output (key=value pairs).
	scanner := bufio.NewScanner(stdout)
	var currentProgress EncodeProgress
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]
		switch key {
		case "frame":
			currentProgress.Frame, _ = strconv.Atoi(val)
		case "out_time_us":
			us, _ := strconv.ParseInt(val, 10, 64)
			currentProgress.Time = float64(us) / 1_000_000.0
			if duration > 0 {
				currentProgress.Percent = currentProgress.Time / duration
				if currentProgress.Percent > 1.0 {
					currentProgress.Percent = 1.0
				}
			}
		case "speed":
			currentProgress.Speed = val
		case "progress":
			// "continue" or "end" — send the accumulated progress.
			select {
			case progressCh <- currentProgress:
			default:
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading ffmpeg output: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg encoding failed: %w", err)
	}

	// Move temp file to final destination.
	if err := os.Rename(tmpDst, dst); err != nil {
		return fmt.Errorf("moving encoded file: %w", err)
	}

	return nil
}

func lastPathComponent(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return path
	}
	return path[i+1:]
}
