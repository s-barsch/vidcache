package main

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
	StatusNeedsCache                   // missing sizes need to be generated
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
