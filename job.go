package main

import "context"

// encodeJob is one piece of missing output for a video: a sized variant, or
// its seek preview.
type encodeJob struct {
	video *VideoFile
	res   Resolution
	seek  bool
}

// jobsFor lists what a video is missing. The seek preview comes last, so it
// can be decoded from the smallest size the same run has just written.
func jobsFor(v *VideoFile) []encodeJob {
	var jobs []encodeJob
	for _, res := range v.MissingSizes {
		jobs = append(jobs, encodeJob{video: v, res: res})
	}
	if !v.HasSeek {
		jobs = append(jobs, encodeJob{video: v, seek: true})
	}
	return jobs
}

// Tag names the output, e.g. "720" or "seek".
func (j encodeJob) Tag() string {
	if j.seek {
		return seekTag
	}
	return j.res.Tag
}

// Target is the file the job writes.
func (j encodeJob) Target() string {
	if j.seek {
		return j.video.SeekTrackPath()
	}
	return j.video.SizedPath(j.res)
}

// Run produces the output, sending progress on progressCh and closing it when done.
func (j encodeJob) Run(ctx context.Context, progressCh chan<- EncodeProgress) error {
	if j.seek {
		return GenerateSeek(ctx, j.video, progressCh)
	}
	return Transcode(ctx, j.video.Path, j.video.SizedPath(j.res),
		j.res.Height, j.video.IsPortrait, j.video.Duration, progressCh)
}
