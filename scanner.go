package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// ScanScope resolves what a scan covers: the whole cache, or the folder named
// by arg, which has to be inside it. A file stands for the folder it is in,
// so a scan always sees every video next to a sizes directory and the dims
// files it finds there without a video are really orphaned.
func ScanScope(cachePath, arg string) (string, error) {
	root, err := filepath.EvalSymlinks(cachePath)
	if err != nil {
		return "", fmt.Errorf("resolving cache path %s: %w", cachePath, err)
	}
	if arg == "" {
		return root, nil
	}

	scope, err := filepath.Abs(arg)
	if err == nil {
		scope, err = filepath.EvalSymlinks(scope)
	}
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", arg, err)
	}
	rel, err := filepath.Rel(root, scope)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is not in the cache %s", arg, root)
	}

	info, err := os.Stat(scope)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		scope = filepath.Dir(scope)
	}
	return scope, nil
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
			// Skip sizes and seek directories — those are output, not input.
			if name == "sizes" || name == seekDirName {
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

	// Clean up orphaned metadata files.
	if progressFn != nil {
		progressFn("Cleaning up orphaned metadata...")
	}

	validMetaFiles := make(map[string]bool)
	sizesDirs := make(map[string]bool)
	for _, v := range result.Videos {
		sDir := filepath.Join(v.Dir, "sizes")
		sizesDirs[sDir] = true
		for _, suffix := range metaSuffixes {
			validMetaFiles[filepath.Join(sDir, v.BaseName+suffix)] = true
		}
	}

	for sDir := range sizesDirs {
		entries, err := os.ReadDir(sDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && isMetaFile(e.Name()) {
				fullPath := filepath.Join(sDir, e.Name())
				if !validMetaFiles[fullPath] {
					os.Remove(fullPath)
				}
			}
		}
	}

	return result, nil
}

const (
	dimsSuffix     = ".dims.txt"
	durationSuffix = ".duration.txt"
)

var metaSuffixes = []string{dimsSuffix, durationSuffix}

func isMetaFile(name string) bool {
	for _, suffix := range metaSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// writeMetaFile writes content to metaPath unless it already exists and is
// newer than the video it describes.
func writeMetaFile(videoPath, metaPath, content string) {
	videoStat, err := os.Stat(videoPath)
	if err != nil {
		return
	}
	if metaStat, err := os.Stat(metaPath); err == nil && !metaStat.ModTime().Before(videoStat.ModTime()) {
		return
	}
	os.MkdirAll(filepath.Dir(metaPath), 0755)
	os.WriteFile(metaPath, []byte(content), 0644)
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

	// Check for captions
	captionsDir := filepath.Join(dir, "captions")
	if stat, err := os.Stat(filepath.Join(captionsDir, baseName+".en.vtt")); err == nil && !stat.IsDir() {
		video.HasCaptionEN = true
	}
	if stat, err := os.Stat(filepath.Join(captionsDir, baseName+".de.vtt")); err == nil && !stat.IsDir() {
		video.HasCaptionDE = true
	}

	// Check for scripts
	scriptDir := filepath.Join(dir, "script")
	if stat, err := os.Stat(filepath.Join(scriptDir, baseName+".en.txt")); err == nil && !stat.IsDir() {
		video.HasScriptEN = true
	}
	if stat, err := os.Stat(filepath.Join(scriptDir, baseName+".de.txt")); err == nil && !stat.IsDir() {
		video.HasScriptDE = true
	}

	// Check for the seek preview: the track counts only beside its sprite.
	video.HasSeek = isRegularFile(video.SeekTrackPath()) && isRegularFile(video.SeekSpritePath())

	// Check if filename has the correct resolution tag.
	if currentTag != actualRes.Tag {
		video.NeedsRename = true
		video.Status = StatusNeedsRename
	}

	// Determine which sizes exist and which are missing.
	smallerSizes := SmallerResolutions(actualRes)
	sizesDir := filepath.Join(dir, "sizes")

	// Ensure the metadata files exist or update them if older than mp4.
	writeMetaFile(path, filepath.Join(sizesDir, baseName+dimsSuffix),
		fmt.Sprintf("%dx%d", probe.Width, probe.Height))
	if probe.Duration > 0 {
		writeMetaFile(path, filepath.Join(sizesDir, baseName+durationSuffix),
			strconv.FormatFloat(probe.Duration, 'f', -1, 64))
	}

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
	} else if video.NeedsWork() {
		video.Status = StatusNeedsCache
	} else {
		video.Status = StatusOK
	}

	return video, nil
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
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
	if video.NeedsWork() {
		video.Status = StatusNeedsCache
	} else {
		video.Status = StatusOK
	}

	return nil
}
