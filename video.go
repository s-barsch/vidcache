package main

import "os"

// Resolution represents a video quality tier.
type Resolution struct {
	Tag    string // "480", "720", "1080", "2k", "4k"
	Height int    // 480, 720, 1080, 1440, 2160
}

// AllResolutions in descending order.
var AllResolutions = []Resolution{
	{Tag: "4k", Height: 2160},
	{Tag: "2k", Height: 1440},
	{Tag: "1080", Height: 1080},
	{Tag: "720", Height: 720},
	{Tag: "480", Height: 480},
}

// ResolutionByTag returns the Resolution for a given tag string.
func ResolutionByTag(tag string) (Resolution, bool) {
	for _, r := range AllResolutions {
		if r.Tag == tag {
			return r, true
		}
	}
	return Resolution{}, false
}

// ResolutionForHeight returns the best matching resolution for a pixel height.
func ResolutionForHeight(height int) Resolution {
	for _, r := range AllResolutions {
		if height >= r.Height {
			return r
		}
	}
	return AllResolutions[len(AllResolutions)-1]
}

// SmallerResolutions returns all resolutions strictly below the given one.
func SmallerResolutions(res Resolution) []Resolution {
	var out []Resolution
	below := false
	for _, r := range AllResolutions {
		if r.Tag == res.Tag {
			below = true
			continue
		}
		if below {
			out = append(out, r)
		}
	}
	return out
}

// VideoStatus tracks the state of a video file through the pipeline.
type VideoStatus int

const (
	StatusOK        VideoStatus = iota // fully cached, filename correct
	StatusNeedsRename                  // filename missing/wrong resolution tag
	StatusNeedsCache                   // missing sizes or seek preview need to be generated
	StatusQueued                       // in the encoding queue
	StatusEncoding                     // currently being encoded
	StatusDone                         // encoding finished this session
	StatusError                        // an error occurred
	StatusSkipped                      // user skipped this file
)

func (s VideoStatus) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusNeedsRename:
		return "rename"
	case StatusNeedsCache:
		return "cache"
	case StatusQueued:
		return "queued"
	case StatusEncoding:
		return "encoding"
	case StatusDone:
		return "done"
	case StatusError:
		return "error"
	case StatusSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// VideoFile holds all information about a discovered video.
type VideoFile struct {
	Path             string       // absolute path to the mp4 file
	Dir              string       // directory containing the file
	Filename         string       // e.g. "240813_121003-4k.mp4"
	OriginalFilename string       // name before any rename happened
	BaseName         string       // name without extension and resolution tag, e.g. "240813_121003"
	CurrentTag       string       // resolution tag currently in filename ("4k", "1080", or "")
	ActualRes        Resolution   // detected resolution from ffprobe
	IsPortrait       bool         // height > width
	Width            int          // pixel width
	Height           int          // pixel height
	Duration         float64      // duration in seconds
	ExistSizes       []Resolution // which resized mp4s exist in "sizes/"
	MissingSizes     []Resolution // which resized mp4s are missing
	NeedsRename      bool         // true if filename needs to be corrected
	Status           VideoStatus  // current status in the pipeline
	Error            string       // error message if Status == StatusError

	// Assets
	HasCaptionEN bool // true if captions/{basename}.en.vtt exists
	HasCaptionDE bool // true if captions/{basename}.de.vtt exists
	HasScriptEN  bool // true if script/{basename}.en.txt exists
	HasScriptDE  bool // true if script/{basename}.de.txt exists
	HasSeek      bool // true if seek/{basename}.vtt and seek/{basename}.jpg exist
}

// NeedsWork reports whether a size or the seek preview is still missing.
func (v *VideoFile) NeedsWork() bool {
	return len(v.MissingSizes) > 0 || !v.HasSeek
}

// PendingTags lists what is still to be generated, e.g. ["720", "480", "seek"].
func (v *VideoFile) PendingTags() []string {
	var tags []string
	for _, res := range v.MissingSizes {
		tags = append(tags, res.Tag)
	}
	if !v.HasSeek {
		tags = append(tags, seekTag)
	}
	return tags
}

// SeekDir returns the path to the seek subdirectory.
func (v *VideoFile) SeekDir() string {
	return v.Dir + "/" + seekDirName
}

// SeekTrackPath returns the path of the seek preview's WebVTT track.
func (v *VideoFile) SeekTrackPath() string {
	return v.SeekDir() + "/" + v.BaseName + ".vtt"
}

// SeekSpritePath returns the path of the seek preview's sprite sheet.
func (v *VideoFile) SeekSpritePath() string {
	return v.SeekDir() + "/" + v.SeekSpriteFilename()
}

// SeekSpriteFilename is how the track refers to its sprite: as a sibling file.
func (v *VideoFile) SeekSpriteFilename() string {
	return v.BaseName + ".jpg"
}

// SmallestSource returns the smallest existing sized variant, falling back to
// the main file, so preview frames are decoded from as few pixels as possible.
func (v *VideoFile) SmallestSource() string {
	sizes := SmallerResolutions(v.ActualRes)
	for i := len(sizes) - 1; i >= 0; i-- {
		path := v.SizedPath(sizes[i])
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return v.Path
}

// SizesDir returns the path to the sizes subdirectory.
func (v *VideoFile) SizesDir() string {
	return v.Dir + "/sizes"
}

// SizedFilename returns the filename for a given resolution.
// e.g. BaseName="240813_121003", res.Tag="1080" → "240813_121003-1080.mp4"
func (v *VideoFile) SizedFilename(res Resolution) string {
	return v.BaseName + "-" + res.Tag + ".mp4"
}

// SizedPath returns the full path for a sized variant.
func (v *VideoFile) SizedPath(res Resolution) string {
	return v.SizesDir() + "/" + v.SizedFilename(res)
}

// CorrectFilename returns what the main file should be named.
func (v *VideoFile) CorrectFilename() string {
	return v.BaseName + "-" + v.ActualRes.Tag + ".mp4"
}

// CorrectPath returns the full path with the correct resolution tag.
func (v *VideoFile) CorrectPath() string {
	return v.Dir + "/" + v.CorrectFilename()
}
