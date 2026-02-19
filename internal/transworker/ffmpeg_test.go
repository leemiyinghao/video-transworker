package transworker

import (
	"os"
	"path"
	"testing"
)

func TestMaxBitrateForQualityLevel(t *testing.T) {
	tests := []struct {
		name        string
		info        VideoInfo
		level       TargetQualityLevel
		minExpected uint // in bps
		maxExpected uint // in bps
	}{
		{
			name:        "4K Low",
			info:        VideoInfo{Width: 3840, Height: 2160},
			level:       TargetQualityLow,
			minExpected: 2000000,
			maxExpected: 2100000,
		},
		{
			name:        "1080p Medium",
			info:        VideoInfo{Width: 1920, Height: 1080},
			level:       TargetQualityMedium,
			minExpected: 2500000,
			maxExpected: 2700000,
		},
		{
			name:        "720p High",
			info:        VideoInfo{Width: 1280, Height: 720},
			level:       TargetQualityHigh,
			minExpected: 3300000,
			maxExpected: 3500000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maxBitrateForQualityLevel(tt.info, tt.level)
			if got < tt.minExpected || got > tt.maxExpected {
				t.Errorf("maxBitrateForQualityLevel() = %v, want between %v and %v", got, tt.minExpected, tt.maxExpected)
			}
		})
	}
}

func TestFFMpegTransWorker_Transcode(t *testing.T) {
	tests := []struct {
		name      string
		inputPath string
	}{
		{
			name:      "MP4 H264",
			inputPath: "testdata/input_h264.mp4",
		},
		{
			name:      "MKV H265",
			inputPath: "testdata/input_h265.mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := os.Stat(tt.inputPath); os.IsNotExist(err) {
				t.Skipf("Skipping test %s: input file %s not found.", tt.name, tt.inputPath)
			}

			tempDir := t.TempDir()
			testInputCopy := path.Join(tempDir, path.Base(tt.inputPath))

			// Determine expected output name based on logic in getOutputFilePathAndName
			_, outputName := getOutputFilePathAndName(testInputCopy)
			testOutputCopy := path.Join(tempDir, outputName)

			inputData, err := os.ReadFile(tt.inputPath)
			if err != nil {
				t.Fatalf("Failed to read test asset: %v", err)
			}
			if err := os.WriteFile(testInputCopy, inputData, 0644); err != nil {
				t.Fatalf("Failed to copy test asset to temp dir: %v", err)
			}

			ctx := t.Context()
			options := TransWorkerOptions{
				MaxConcurrency: 1,
				TargetQuality:  TargetQualityMedium,
				MaxResolution:  MaxResolution{Width: 1920, Height: 1080},
				MaxFps:         60,
			}
			worker := NewFFMpegTransWorker(ctx, options)

			task := TranscodingTaskInput{
				Files: []string{testInputCopy},
			}

			if _, err := worker.Dispatch(ctx, task); err != nil {
				t.Fatalf("Failed to dispatch task: %v", err)
			}

			finished, result := worker.WaitTask(ctx)
			if finished {
				t.Fatal("Worker finished unexpectedly without returning result")
			}

			if result.Err != nil {
				t.Fatalf("Transcoding failed: %v", result.Err)
			}

			if _, err := os.Stat(testOutputCopy); os.IsNotExist(err) {
				t.Errorf("Expected output file %s was not created", testOutputCopy)
			}

			// Verify transcoded metadata exists in result
			if info, ok := result.TranscodedFiles[testOutputCopy]; !ok {
				t.Errorf("Result does not contain info for %s", testOutputCopy)
			} else {
				if info.Bitrate < 200000 {
					t.Errorf("Transcoded bitrate too low: %d", info.Bitrate)
				}
				// debug output info
				t.Logf("Transcoded file info: Codec=%s, Resolution=%dx%d, Fps=%.2f, Bitrate=%d", info.Codec, info.Width, info.Height, info.Fps, info.Bitrate)
			}
		})
	}
}
