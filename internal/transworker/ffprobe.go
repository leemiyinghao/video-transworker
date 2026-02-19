package transworker

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/vansante/go-ffprobe"
)

func peekVideoInfo(ctx context.Context, filePath string) (*VideoInfo, error) {
	probeData, err := ffprobe.GetProbeDataContext(ctx, filePath)
	if err != nil {
		return nil, err
	}
	vi := VideoInfo{
		FilePath: filePath,
	}
	// check file size
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}
	vi.FileSize = uint64(fileInfo.Size())
	firstVideoStream := probeData.GetFirstVideoStream()
	if firstVideoStream == nil {
		return nil, fmt.Errorf("no video stream found in file: %s", filePath)
	}
	vi.Codec = parseCodec(firstVideoStream.CodecName)
	vi.Width = uint(firstVideoStream.Width)
	vi.Height = uint(firstVideoStream.Height)
	if fps, err := strconv.ParseFloat(firstVideoStream.AvgFrameRate, 64); err == nil {
		vi.Fps = fps
	} else {
		// assume 30 fps if parsing fails
		vi.Fps = 30.0
	}
	if bitrate, err := strconv.ParseUint(firstVideoStream.BitRate, 10, 64); err == nil {
		vi.Bitrate = bitrate
	} else {
		// if BitRate is not available, try to calculate it from file size and duration
		if probeData.Format.DurationSeconds > 0 {
			vi.Bitrate = uint64(float64(vi.FileSize*8) / probeData.Format.DurationSeconds)
		} else {
			vi.Bitrate = 0
		}
	}
	return &vi, nil
}

func parseCodec(codecName string) Codec {
	switch codecName {
	case "av1":
		return CodecAV1
	case "h264":
		return CodecH264
	case "h265", "hevc":
		return CodecH265
	case "vp9":
		return CodecVP9
	default:
		return CodecUnknown
	}
}
