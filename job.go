package main

import "context"

// encodeJob is one piece of missing output for a video: a sized variant, its
// seek preview, or its HLS ladder.
type encodeJob struct {
	video *VideoFile
	res   Resolution
	seek  bool
	hls   bool
}

// jobsFor lists what a video is missing. The seek preview comes after the
// sizes, so it can be decoded from the smallest size the same run has just
// written.
func jobsFor(v *VideoFile) []encodeJob {
	var jobs []encodeJob
	for _, res := range v.MissingSizes {
		jobs = append(jobs, encodeJob{video: v, res: res})
	}
	if !v.HasSeek {
		jobs = append(jobs, encodeJob{video: v, seek: true})
	}
	if !v.HasHLS {
		jobs = append(jobs, encodeJob{video: v, hls: true})
	}
	return jobs
}

// Tag names the output, e.g. "720", "seek" or "hls".
func (j encodeJob) Tag() string {
	switch {
	case j.seek:
		return seekTag
	case j.hls:
		return hlsTag
	}
	return j.res.Tag
}

// Target is the file the job writes.
func (j encodeJob) Target() string {
	switch {
	case j.seek:
		return j.video.SeekTrackPath()
	case j.hls:
		return j.video.HLSPlaylistPath()
	}
	return j.video.SizedPath(j.res)
}

// Run produces the output, sending progress on progressCh and closing it when done.
func (j encodeJob) Run(ctx context.Context, progressCh chan<- EncodeProgress) error {
	switch {
	case j.seek:
		return GenerateSeek(ctx, j.video, progressCh)
	case j.hls:
		return GenerateHLS(ctx, j.video, progressCh)
	}
	return Transcode(ctx, j.video.Path, j.video.SizedPath(j.res),
		j.res.Height, j.video.IsPortrait, j.video.Duration, progressCh)
}
