package main

import (
	"slices"
	"strings"
	"testing"
)

func hlsVideo(height int, portrait, audio bool) *VideoFile {
	return &VideoFile{
		Path:       "/cache/20/20-08/04/200804_170415-1080.mp4",
		Dir:        "/cache/20/20-08/04",
		BaseName:   "200804_170415",
		ActualRes:  ResolutionForHeight(height),
		IsPortrait: portrait,
		HasAudio:   audio,
	}
}

// argAfter is the value that follows flag in args.
func argAfter(t *testing.T, args []string, flag string) string {
	t.Helper()
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("no %s in %v", flag, args)
	}
	return args[i+1]
}

func TestHLSLadderStartsAtTheVideosOwnResolution(t *testing.T) {
	var tags []string
	for _, res := range hlsVideo(1080, false, true).HLSLadder() {
		tags = append(tags, res.Tag)
	}

	if got := strings.Join(tags, " "); got != "1080 720 480" {
		t.Fatalf("got ladder %q, want 1080 720 480", got)
	}
}

func TestHLSArgsNameEveryRenditionByItsTag(t *testing.T) {
	args := hlsArgs(hlsVideo(1080, false, true), "/tmp/x")

	if got := argAfter(t, args, "-var_stream_map"); got != "v:0,a:0,name:1080 v:1,a:1,name:720 v:2,a:2,name:480" {
		t.Fatalf("got stream map %q", got)
	}
	if got := argAfter(t, args, "-master_pl_name"); got != "200804_170415.m3u8" {
		t.Fatalf("got master %q", got)
	}
	if got := argAfter(t, args, "-hls_segment_filename"); got != "/tmp/x/200804_170415-%v.mp4" {
		t.Fatalf("got segment filename %q", got)
	}
	if got := args[len(args)-1]; got != "/tmp/x/200804_170415-%v.m3u8" {
		t.Fatalf("got playlist %q", got)
	}
}

func TestHLSArgsScalePortraitByWidth(t *testing.T) {
	args := hlsArgs(hlsVideo(1080, true, true), "/tmp/x")

	want := "[0:v]split=3[s0][s1][s2];[s0]scale=1080:-2[v0];[s1]scale=720:-2[v1];[s2]scale=480:-2[v2]"
	if got := argAfter(t, args, "-filter_complex"); got != want {
		t.Fatalf("got filter %q, want %q", got, want)
	}
}

func TestHLSArgsCapEachRenditionAtItsRate(t *testing.T) {
	args := hlsArgs(hlsVideo(1080, false, true), "/tmp/x")

	if got := argAfter(t, args, "-maxrate:v:1"); got != "3500k" {
		t.Fatalf("got 720 maxrate %q, want 3500k", got)
	}
	if got := argAfter(t, args, "-bufsize:v:1"); got != "7000k" {
		t.Fatalf("got 720 bufsize %q, want 7000k", got)
	}
}

func TestHLSArgsWithoutSoundMapNoAudio(t *testing.T) {
	args := hlsArgs(hlsVideo(720, false, false), "/tmp/x")

	if slices.Contains(args, "0:a:0") || slices.Contains(args, "-c:a") {
		t.Fatalf("silent video maps audio: %v", args)
	}
	if got := argAfter(t, args, "-var_stream_map"); got != "v:0,name:720 v:1,name:480" {
		t.Fatalf("got stream map %q", got)
	}
}

func TestPendingTagsEndWithHLS(t *testing.T) {
	v := hlsVideo(1080, false, true)
	v.HasSeek = true

	if got := v.PendingTags(); !slices.Equal(got, []string{"hls"}) {
		t.Fatalf("got %v, want [hls]", got)
	}
	if !v.NeedsWork() {
		t.Fatal("a video without its HLS ladder needs no work")
	}
}
