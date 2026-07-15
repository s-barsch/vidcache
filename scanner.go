package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScanResult holds the output of scanning the cache directory.
type ScanResult struct {
	Videos     []*VideoFile
	OKCount    int
	RenameCount int
	CacheCount int
	Errors     []string
}

// ScanVideos walks the cache directory and discovers all main mp4 files,
// probes their resolution, checks filename conventions, and determines
// which sizes are missing.
func ScanVideos(cachePath string, progressFn func(msg string)) (*ScanResult, error) {
	result := &ScanResult{}

	// Resolve symlinks for the root path.
	resolvedRoot, err := filepath.EvalSymlinks(cachePath)
	if err != nil {
		return nil, fmt.Errorf("resolving cache path %s: %w", cachePath, err)
	}

	// Collect all mp4 files first.
	var mp4Paths []string
	err = filepath.Walk(resolvedRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}

		// Skip bot directories.
		if info.IsDir() {
			name := info.Name()
			if name == "bot" || name == ".bot" {
				return filepath.SkipDir
			}
			// Skip sizes directories — those are output, not input.
			if name == "sizes" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process mp4 files.
		if strings.ToLower(filepath.Ext(path)) == ".mp4" {
			mp4Paths = append(mp4Paths, path)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", resolvedRoot, err)
	}

	// Process each mp4 file.
	for i, path := range mp4Paths {
		if progressFn != nil {
			progressFn(fmt.Sprintf("Probing %d/%d: %s", i+1, len(mp4Paths), filepath.Base(path)))
		}

		video, err := analyzeVideo(path)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", filepath.Base(path), err))
			continue
		}

		result.Videos = append(result.Videos, video)

		switch video.Status {
		case StatusOK:
			result.OKCount++
		case StatusNeedsRename:
			result.RenameCount++
		case StatusNeedsCache:
			result.CacheCount++
		}
	}

	return result, nil
}

// analyzeVideo probes a single video file and determines its state.
func analyzeVideo(path string) (*VideoFile, error) {
	probe, err := Probe(path)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)
	filename := filepath.Base(path)
	ext := filepath.Ext(filename)
	nameNoExt := strings.TrimSuffix(filename, ext)

	// Parse existing resolution tag from filename.
	baseName, currentTag := parseResolutionTag(nameNoExt)

	// Determine actual resolution.
	isPortrait := probe.Height > probe.Width
	var effectiveHeight int
	if isPortrait {
		effectiveHeight = probe.Width // for portrait, the shorter dimension is width
	} else {
		effectiveHeight = probe.Height
	}
	actualRes := ResolutionForHeight(effectiveHeight)

	video := &VideoFile{
		Path:             path,
		Dir:              dir,
		Filename:         filename,
		OriginalFilename: filename,
		BaseName:         baseName,
		CurrentTag:       currentTag,
		ActualRes:        actualRes,
		IsPortrait:       isPortrait,
		Width:            probe.Width,
		Height:           probe.Height,
		Duration:         probe.Duration,
	}

	// Check if filename has the correct resolution tag.
	if currentTag != actualRes.Tag {
		video.NeedsRename = true
		video.Status = StatusNeedsRename
	}

	// Determine which sizes exist and which are missing.
	smallerSizes := SmallerResolutions(actualRes)
	sizesDir := filepath.Join(dir, "sizes")

	for _, res := range smallerSizes {
		sizedPath := filepath.Join(sizesDir, baseName+"-"+res.Tag+".mp4")
		if _, err := os.Stat(sizedPath); err == nil {
			video.ExistSizes = append(video.ExistSizes, res)
		} else {
			video.MissingSizes = append(video.MissingSizes, res)
		}
	}

	// Determine final status.
	if video.NeedsRename {
		video.Status = StatusNeedsRename
	} else if len(video.MissingSizes) > 0 {
		video.Status = StatusNeedsCache
	} else {
		video.Status = StatusOK
	}

	return video, nil
}

// parseResolutionTag splits "240813_121003-4k" into ("240813_121003", "4k").
// If no known tag is found, returns (fullName, "").
func parseResolutionTag(nameNoExt string) (baseName, tag string) {
	for _, res := range AllResolutions {
		suffix := "-" + res.Tag
		if strings.HasSuffix(nameNoExt, suffix) {
			return strings.TrimSuffix(nameNoExt, suffix), res.Tag
		}
	}
	return nameNoExt, ""
}

// RenameVideo renames a video file to include the correct resolution tag.
func RenameVideo(video *VideoFile) error {
	newPath := video.CorrectPath()
	if newPath == video.Path {
		return nil
	}

	if err := os.Rename(video.Path, newPath); err != nil {
		return fmt.Errorf("renaming %s → %s: %w", video.Filename, video.CorrectFilename(), err)
	}

	// Update the video struct.
	video.Path = newPath
	video.Filename = video.CorrectFilename()
	video.CurrentTag = video.ActualRes.Tag
	video.NeedsRename = false

	// Re-evaluate status.
	if len(video.MissingSizes) > 0 {
		video.Status = StatusNeedsCache
	} else {
		video.Status = StatusOK
	}

	return nil
}
