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
	HasAudio bool
}

// Probe calls ffprobe to get video dimensions, duration, and whether there is
// sound.
func Probe(path string) (probeResult, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_entries", "stream=codec_type,width,height",
		"-show_entries", "format=duration",
		path,
	)

	out, err := cmd.Output()
	if err != nil {
		return probeResult{}, fmt.Errorf("ffprobe %s: %w", path, err)
	}

	var data struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}

	if err := json.Unmarshal(out, &data); err != nil {
		return probeResult{}, fmt.Errorf("parsing ffprobe output for %s: %w", path, err)
	}

	var result probeResult
	foundVideo := false
	for _, s := range data.Streams {
		switch {
		case s.CodecType == "video" && !foundVideo:
			result.Width, result.Height = s.Width, s.Height
			foundVideo = true
		case s.CodecType == "audio":
			result.HasAudio = true
		}
	}

	if !foundVideo {
		return probeResult{}, fmt.Errorf("no video stream found in %s", path)
	}

	result.Duration, _ = strconv.ParseFloat(data.Format.Duration, 64)

	return result, nil
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

	// Use a temp file to avoid partial outputs.
	tmpDst := dst + ".tmp.mp4"
	defer os.Remove(tmpDst)

	args := []string{
		"-i", src,
		"-vf", scaleFilter(targetHeight, isPortrait),
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "23",
		"-c:a", "copy",
		"-movflags", "+faststart",
		"-y",
		tmpDst,
	}

	if err := runFFmpeg(ctx, args, duration, progressCh); err != nil {
		return err
	}

	// Move temp file to final destination.
	if err := os.Rename(tmpDst, dst); err != nil {
		return fmt.Errorf("moving encoded file: %w", err)
	}

	return nil
}

// scaleFilter scales to a resolution tier, preserving orientation.
// -2 ensures the other dimension is divisible by 2 (required by most codecs).
func scaleFilter(targetHeight int, isPortrait bool) string {
	if isPortrait {
		// Portrait: the resolution tag refers to the width.
		// e.g. -720 on a 1080×1920 portrait → 720×1280.
		return fmt.Sprintf("scale=%d:-2", targetHeight)
	}
	// Landscape: the resolution tag refers to the height.
	// e.g. -1080 on a 3840×2160 landscape → 1920×1080.
	return fmt.Sprintf("scale=-2:%d", targetHeight)
}

// runFFmpeg runs ffmpeg with args and sends its progress on progressCh,
// measured against duration, until it exits.
func runFFmpeg(ctx context.Context, args []string, duration float64, progressCh chan<- EncodeProgress) error {
	args = append([]string{"-progress", "pipe:1", "-nostats"}, args...)

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

	return nil
}

func lastPathComponent(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return path
	}
	return path[i+1:]
}
