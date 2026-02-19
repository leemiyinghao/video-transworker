package transworker

import (
	"context"
	"errors"
)

type TargetQualityLevel int

const (
	TargetQualityLow TargetQualityLevel = iota
	TargetQualityMedium
	TargetQualityHigh
)

type MaxResolution struct {
	Width  uint
	Height uint
}

type Codec string

const (
	CodecAV1     Codec = "av1"
	CodecH264    Codec = "h264"
	CodecH265    Codec = "h265"
	CodecVP9     Codec = "vp9"
	CodecUnknown Codec = "unknown"
)

type TransWorkerOptions struct {
	MaxConcurrency int
	TargetQuality  TargetQualityLevel
	MaxResolution  MaxResolution
	MaxFps         uint
	SkipCodecs     []Codec
}

type TranscodingTaskInput struct {
	Files []string
}

type ParsedTranscodingTaskInput struct {
	TranscodingTaskInput
	Infos map[string]VideoInfo
}

type VideoInfo struct {
	Codec    Codec
	FilePath string
	FileSize uint64
	Width    uint
	Height   uint
	Fps      float64
	Bitrate  uint64
}

type TranscodingResult struct {
	OriginalFiles   map[string]VideoInfo
	TranscodedFiles map[string]VideoInfo
	Err             error
}

var (
	ErrNoFilesToTranscode = errors.New("no files to transcode")
)

type TransWorker interface {
	Dispatch(ctx context.Context, task TranscodingTaskInput) (bool, error)
	// WaitTask waits for the current task to complete and returns whether the task is finished, the transcoding result if available, and any error that occurred during transcoding. If the context is canceled while waiting, it should return immediately with an appropriate error.
	WaitTask(ctx context.Context) (bool, *TranscodingResult)
}
