package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	hlsDirName = "hls"
	hlsTag     = "hls"

	// hlsSegment is the length of a segment in seconds, and the keyframe
	// interval of every rendition, so a player can switch at any segment.
	hlsSegment = 4
)

// HLSDir returns the path to the hls subdirectory.
func (v *VideoFile) HLSDir() string {
	return v.Dir + "/" + hlsDirName
}

// HLSPlaylistPath returns the path of the master playlist.
func (v *VideoFile) HLSPlaylistPath() string {
	return v.HLSDir() + "/" + v.BaseName + ".m3u8"
}

// HLSLadder is every rendition of the stream: the video's own resolution and
// each one below it.
func (v *VideoFile) HLSLadder() []Resolution {
	return append([]Resolution{v.ActualRes}, SmallerResolutions(v.ActualRes)...)
}

// hlsArgs encodes the original into every rendition of the ladder at once,
// written to dir as one fragmented mp4 per rendition, each with its own
// playlist of byte ranges, and the master playlist that lists them:
//
//	240813_121003.m3u8
//	240813_121003-1080.m3u8  240813_121003-1080.mp4
//	240813_121003-720.m3u8   240813_121003-720.mp4
//
// Every playlist names its files as siblings.
func hlsArgs(v *VideoFile, dir string) []string {
	ladder := v.HLSLadder()

	split := fmt.Sprintf("[0:v]split=%d", len(ladder))
	scales := make([]string, 0, len(ladder))
	for i, res := range ladder {
		split += fmt.Sprintf("[s%d]", i)
		scales = append(scales, fmt.Sprintf("[s%d]%s[v%d]", i, scaleFilter(res.Height, v.IsPortrait), i))
	}

	args := []string{
		"-i", v.Path,
		"-filter_complex", split + ";" + strings.Join(scales, ";"),
	}

	streams := make([]string, 0, len(ladder))
	for i, res := range ladder {
		args = append(args, "-map", fmt.Sprintf("[v%d]", i))
		stream := fmt.Sprintf("v:%d", i)
		if v.HasAudio {
			args = append(args, "-map", "0:a:0")
			stream += fmt.Sprintf(",a:%d", i)
		}
		streams = append(streams, stream+",name:"+res.Tag)
	}

	args = append(args,
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "23",
		"-pix_fmt", "yuv420p",
		"-sc_threshold", "0",
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", hlsSegment),
	)
	for i, res := range ladder {
		args = append(args,
			fmt.Sprintf("-maxrate:v:%d", i), strconv.Itoa(res.MaxRate)+"k",
			fmt.Sprintf("-bufsize:v:%d", i), strconv.Itoa(2*res.MaxRate)+"k",
		)
	}
	if v.HasAudio {
		args = append(args, "-c:a", "copy")
	}

	return append(args,
		"-f", "hls",
		"-hls_time", strconv.Itoa(hlsSegment),
		"-hls_playlist_type", "vod",
		"-hls_segment_type", "fmp4",
		"-hls_flags", "single_file",
		"-var_stream_map", strings.Join(streams, " "),
		"-master_pl_name", v.BaseName+".m3u8",
		"-hls_segment_filename", filepath.Join(dir, v.BaseName+"-%v.mp4"),
		"-y",
		filepath.Join(dir, v.BaseName+"-%v.m3u8"),
	)
}

// GenerateHLS writes the HLS ladder of a video into a temporary folder beside
// hls/, then moves the renditions in and the master playlist last, so a run
// stopped halfway leaves no master behind and the ladder counts as missing.
func GenerateHLS(ctx context.Context, v *VideoFile, progressCh chan<- EncodeProgress) error {
	defer close(progressCh)

	if err := os.MkdirAll(v.HLSDir(), 0o755); err != nil {
		return fmt.Errorf("creating hls dir: %w", err)
	}

	tmp := filepath.Join(v.HLSDir(), v.BaseName+".tmp")
	if err := os.RemoveAll(tmp); err != nil {
		return fmt.Errorf("clearing %s: %w", tmp, err)
	}
	if err := os.Mkdir(tmp, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", tmp, err)
	}
	defer os.RemoveAll(tmp)

	if err := runFFmpeg(ctx, hlsArgs(v, tmp), v.Duration, progressCh); err != nil {
		return err
	}

	master := v.BaseName + ".m3u8"
	var names []string
	for _, res := range v.HLSLadder() {
		names = append(names, v.BaseName+"-"+res.Tag+".mp4", v.BaseName+"-"+res.Tag+".m3u8")
	}
	names = append(names, master)

	for _, name := range names {
		if err := os.Rename(filepath.Join(tmp, name), filepath.Join(v.HLSDir(), name)); err != nil {
			return fmt.Errorf("moving %s: %w", name, err)
		}
	}

	return nil
}
