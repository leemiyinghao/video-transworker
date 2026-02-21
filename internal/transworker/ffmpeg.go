package transworker

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

type FFMpegTransWorker struct {
	taskCh   chan ParsedTranscodingTaskInput
	resultCh chan TranscodingResult
	options  TransWorkerOptions
}

func NewFFMpegTransWorker(ctx context.Context, options TransWorkerOptions) *FFMpegTransWorker {
	worker := FFMpegTransWorker{
		taskCh:   make(chan ParsedTranscodingTaskInput, 10240),
		resultCh: make(chan TranscodingResult, 10240),
		options:  options,
	}
	for i := 0; i < options.MaxConcurrency; i++ {
		go worker.run(ctx)
	}
	return &worker
}

func (w *FFMpegTransWorker) run(ctx context.Context) {
	defer func() {
		fmt.Println("worker exiting")
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-w.taskCh:
			if !ok {
				return
			}
			result := w.transcode(ctx, task)
			w.resultCh <- result
		}
	}
}

// parseTaskInput parses the transcoding task input and returns a structured representation of the input. Return nil if the input does not need transcoding, return an error if the input is invalid or cannot be parsed.
func (w *FFMpegTransWorker) parseTaskInput(ctx context.Context, task TranscodingTaskInput) (*ParsedTranscodingTaskInput, error) {
	result := ParsedTranscodingTaskInput{
		TranscodingTaskInput: task,
		Infos:                make(map[string]VideoInfo),
	}
	for _, file := range task.Files {
		videoInfo, err := peekVideoInfo(ctx, file)
		if err != nil || videoInfo == nil {
			continue
		}
		result.Infos[file] = *videoInfo
	}
	if len(result.Infos) == 0 {
		return nil, ErrNoFilesToTranscode
	}

	// check if the files need to be transcoded, if not, return the original files as transcoded files
	needTranscode := len(result.Infos) > 1 // if there are more than one file, we need to transcode to concatenate them into one file
	if !needTranscode {
		for _, videoInfo := range result.Infos {
			if !slices.Contains(w.options.SkipCodecs, videoInfo.Codec) {
				needTranscode = true
				break
			}
			if videoInfo.Width > w.options.MaxResolution.Width || videoInfo.Height > w.options.MaxResolution.Height {
				needTranscode = true
				break
			}
			if videoInfo.Fps > float64(w.options.MaxFps) {
				needTranscode = true
				break
			}
		}
	}
	if !needTranscode {
		return nil, nil
	}

	return &result, nil
}

func (w *FFMpegTransWorker) transcode(ctx context.Context, task ParsedTranscodingTaskInput) TranscodingResult {
	// prepare result
	result := TranscodingResult{
		OriginalFiles: task.Infos,
	}

	// prepare temp file without opening it
	tempOutputDir, err := os.MkdirTemp("", "ffmpeg_transcode_*")
	if err != nil {
		result.Err = err
		return result
	}
	defer os.RemoveAll(tempOutputDir)
	// use the first file name as the base for the temp output file
	outputFilePath, outputFileName := getOutputFilePathAndName(task.Files[0])
	tempOutputFilePath := path.Join(tempOutputDir, outputFileName)

	globalQuality := 60
	// determine the global quality based on the target quality level and the max bitrate for the original files
	switch w.options.TargetQuality {
	case TargetQualityLow:
		globalQuality = 100
	case TargetQualityMedium:
		globalQuality = 60
	case TargetQualityHigh:
		globalQuality = 40
	}

	// transcode the files using ffmpeg, concatenate the input files if there are more than one, and output to the temp output file
	transcodeErr := error(nil)
	if len(task.Files) == 1 {
		maxFps := uint(0)
		if task.Infos[task.Files[0]].Fps > float64(w.options.MaxFps) {
			maxFps = w.options.MaxFps
		}
		maxResolution := MaxResolution{
			Width:  0,
			Height: 0,
		}
		if task.Infos[task.Files[0]].Width > w.options.MaxResolution.Width || task.Infos[task.Files[0]].Height > w.options.MaxResolution.Height {
			maxResolution = w.options.MaxResolution
		}
		transcodeErr = monoFileFFPipeline(ctx, task.Files[0], tempOutputFilePath, globalQuality, maxBitrateForQualityLevel(result.OriginalFiles[task.Files[0]], w.options.TargetQuality), float64(maxFps), maxResolution)
	} else {
		// always resize and change fps for multiple files to avoid issues when concatenating
		transcodeErr = multiFileFFPipeline(ctx, task.Files, tempOutputFilePath, globalQuality, maxBitrateForQualityLevel(result.OriginalFiles[task.Files[0]], w.options.TargetQuality), float64(w.options.MaxFps), w.options.MaxResolution)
	}

	if transcodeErr != nil {
		result.Err = transcodeErr
		return result
	}

	// check if the temp output file is created and can be read, if not, return an error
	if _, err := os.Stat(tempOutputFilePath); err != nil {
		result.Err = err
		return result
	}
	// load the video info of the temp output file, verify bitrate at least 200 kbps
	tempTranscodedVideoInfo, err := peekVideoInfo(ctx, tempOutputFilePath)
	if err != nil {
		result.Err = err
		return result
	}
	if tempTranscodedVideoInfo.Bitrate < 200000 {
		result.Err = fmt.Errorf("transcoded file bitrate is too low: %d bps", tempTranscodedVideoInfo.Bitrate)
		return result
	}

	// open the temp output file to make sure it is created and can be read, and then write it to the original output file path
	source, err := os.Open(tempOutputFilePath)
	if err != nil {
		result.Err = err
		return result
	}
	defer source.Close()

	dest, err := os.OpenFile(outputFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		result.Err = err
		return result
	}
	defer dest.Close()

	if _, err := io.Copy(dest, source); err != nil {
		result.Err = err
		return result
	}

	// remove the source files if they are different from the output file, and update the transcoded files in the result
	for _, file := range task.Files {
		if file == outputFilePath {
			continue
		}
		if err := os.Remove(file); err != nil {
			result.Err = err
			return result
		}
	}

	// recheck the video info of the output file and update the transcoded files in the result
	finalTranscodedVideoInfo, err := peekVideoInfo(ctx, outputFilePath)
	if err != nil {
		result.Err = err
		return result
	}
	result.TranscodedFiles = map[string]VideoInfo{
		outputFilePath: *finalTranscodedVideoInfo,
	}
	return result
}

func monoFileFFPipeline(ctx context.Context, inputFilePath string, outputFilePath string, globalQuality int, maxBitrate uint, maxFps float64, maxResolution MaxResolution) error {
	needCPUProcessing := maxFps > 0 || (maxResolution.Width > 0 && maxResolution.Height > 0)
	var input *ffmpeg.Stream
	if needCPUProcessing {
		input = ffmpeg.Input(inputFilePath, ffmpeg.KwArgs{"init_hw_device": "vaapi=gpu"})
		if maxFps > 0 {
			input = input.Filter("fps", ffmpeg.Args{fmt.Sprintf("fps=%f", maxFps)})
		}
		if maxResolution.Width > 0 && maxResolution.Height > 0 {
			input = input.Filter("scale", ffmpeg.Args{fmt.Sprintf("w='min(%d,iw)':h='min(%d,ih)':force_original_aspect_ratio=decrease", maxResolution.Width, maxResolution.Height)})
		}
		input = input.Filter("format", ffmpeg.Args{"nv12"}).Filter("hwupload", nil) // Standard VAAPI bridge
	} else {
		input = ffmpeg.Input(inputFilePath, ffmpeg.KwArgs{"hwaccel": "vaapi", "hwaccel_device": "/dev/dri/renderD128", "hwaccel_output_format": "vaapi"})
	}

	outputKwargs := ffmpeg.KwArgs{
		"c:v":            "av1_vaapi",
		"global_quality": strconv.Itoa(globalQuality),
		"rc_mode":        "QVBR",
		"c:a":            "copy",
		"c:s":            "copy",
		"map:a":          "0:a?",
		"map:s":          "0:s?",
		"async_depth":    "8",
		"b:v":            strconv.Itoa(int(maxBitrate)/1000) + "k",
		"maxrate":        strconv.Itoa(int(maxBitrate)/1000) + "k",
	}
	if !needCPUProcessing {
		outputKwargs["map:v"] = "0:v:0"
	}

	return ffmpeg.
		OutputContext(ctx,
			[]*ffmpeg.Stream{input},
			outputFilePath,
			outputKwargs,
		).
		OverWriteOutput().
		Run()
}

func multiFileFFPipeline(ctx context.Context, inputFilePaths []string, outputFilePath string, globalQuality int, maxBitrate uint, maxFps float64, maxResolution MaxResolution) error {
	txtFile, _ := os.CreateTemp("", "ffmpeg_concat_*.txt")
	defer os.Remove(txtFile.Name())
	for _, path := range inputFilePaths {
		absPath, _ := filepath.Abs(path)
		fmt.Fprintf(txtFile, "file '%s'\n", absPath)
	}
	txtFile.Close()

	vInput := ffmpeg.Input(txtFile.Name(), ffmpeg.KwArgs{"f": "concat", "safe": "0", "init_hw_device": "vaapi=gpu"})
	aInput := ffmpeg.Input(txtFile.Name(), ffmpeg.KwArgs{"f": "concat", "safe": "0"})

	// 3. Process the Video Branch
	vProcessed := vInput.Video()
	if maxFps > 0 {
		vProcessed = vProcessed.Filter("fps", ffmpeg.Args{fmt.Sprintf("fps=%f", maxFps)})
	}
	if maxResolution.Width > 0 && maxResolution.Height > 0 {
		vProcessed = vProcessed.Filter("scale", ffmpeg.Args{fmt.Sprintf("w='min(%d,iw)':h='min(%d,ih)':force_original_aspect_ratio=decrease", maxResolution.Width, maxResolution.Height)})
	}
	// Final Hardware Bridge
	vProcessed = vProcessed.Filter("format", ffmpeg.Args{"nv12"}).Filter("hwupload", nil)

	return ffmpeg.OutputContext(ctx,
		[]*ffmpeg.Stream{vProcessed, aInput.Audio()},
		outputFilePath,
		ffmpeg.KwArgs{
			"c:v":            "av1_vaapi",
			"c:a":            "aac",
			"global_quality": strconv.Itoa(globalQuality),
			"rc_mode":        "QVBR",
			"async_depth":    "8",
			"b:v":            fmt.Sprintf("%dk", maxBitrate/1000),
			"maxrate":        fmt.Sprintf("%dk", maxBitrate/1000),
		},
	).OverWriteOutput().Run()
}

func getOutputFilePathAndName(inputFilePath string) (string, string) {
	// split the input file path into directory, base name and extension
	dir, file := path.Split(inputFilePath)
	ext := path.Ext(file)
	baseName := file[:len(file)-len(ext)]
	// replace the extension with .mkv or .mp4 depending on the original extension. if the original extension is already .mkv or .mp4, keep it as is, otherwise change it to .mkv
	if ext == ".mkv" || ext == ".mp4" {
		return dir + baseName + ext, baseName + ext
	}
	return dir + baseName + ".mkv", baseName + ".mkv"
}

func maxBitrateForQualityLevel(videoInfo VideoInfo, level TargetQualityLevel) uint {
	// for standard 4k:
	// - low quality: 2 Mbps
	// - medium quality: 5 Mbps
	// - high quality: 10 Mbps
	// for 1080p:
	// - low quality: 1 Mbps
	// - medium quality: 3 Mbps
	// - high quality: 5 Mbps
	// for 720p:
	// - low quality: 0.5 Mbps
	// - medium quality: 1.5 Mbps
	// - high quality: 3 Mbps
	// we use the resolution to determine the bitrate, and we use the target quality level to determine the multiplier for the bitrate

	pixels := float64(videoInfo.Width * videoInfo.Height)
	sqrtPixels := math.Sqrt(pixels)

	var multiplier float64
	switch level {
	case TargetQualityLow:
		multiplier = 700.0
	case TargetQualityMedium:
		multiplier = 1800.0
	case TargetQualityHigh:
		multiplier = 3500.0
	}

	return uint(sqrtPixels * multiplier)
}

// Dispatch dispatches a transcoding task to the worker. Return (needTranscode, error)
func (w *FFMpegTransWorker) Dispatch(ctx context.Context, task TranscodingTaskInput) (bool, error) {
	parsedTask, err := w.parseTaskInput(ctx, task)
	if err != nil {
		return false, err
	}
	if parsedTask == nil {
		return false, nil
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case w.taskCh <- *parsedTask:
		return true, nil
	}
}

// WaitTask waits for the transcoding result of the dispatched task, and returns a boolean indicating if all tasks are finished, the transcoding result if available, and any error that occurred during transcoding. If the context is canceled while waiting, it should return immediately with an appropriate error.
func (w *FFMpegTransWorker) WaitTask(ctx context.Context) (bool, *TranscodingResult) {
	select {
	case <-ctx.Done():
		// close the channels to stop the worker goroutines
		close(w.taskCh)
		close(w.resultCh)
		return true, nil
	case result, ok := <-w.resultCh:
		if !ok {
			return true, nil
		}
		return false, &result
	}
}

// interface guard
var _ TransWorker = (*FFMpegTransWorker)(nil)
