package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"strings"

	"github.com/leemiyinghao/video-transworker/internal/transworker"
	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

func main() {
	var config string

	var rootCmd = &cobra.Command{
		Use:   "transworker <path>",
		Short: "Video transworker CLI",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			path := args[0]
			// Logic of running the command will be placed in different module.
			fmt.Printf("Processing path: %s with config: %s\n", path, config)
			// load the config
			conf, err := LoadConfig(config)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
				os.Exit(1)
			}
			// dispatch tasks and wait for results
			dispatchAndWait(path, conf)
		},
	}

	rootCmd.PersistentFlags().StringVarP(&config, "config", "c", "", "Path to configuration file")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (c *CliConfig) asTWConfig() transworker.TransWorkerOptions {
	targetQuality := transworker.TargetQualityMedium
	switch c.TargetQuality {
	case TargetQualityLevelLow:
		targetQuality = transworker.TargetQualityLow
	case TargetQualityLevelMedium:
		targetQuality = transworker.TargetQualityMedium
	case TargetQualityLevelHigh:
		targetQuality = transworker.TargetQualityHigh
	}
	maxResolution := transworker.MaxResolution{
		Width:  c.MaxResolution.Width,
		Height: c.MaxResolution.Height,
	}
	skipCodecs := make([]transworker.Codec, 0, len(c.SkipCodecs))
	for _, codec := range c.SkipCodecs {
		switch codec {
		case CodecH264:
			skipCodecs = append(skipCodecs, transworker.CodecH264)
		case CodecH265:
			skipCodecs = append(skipCodecs, transworker.CodecH265)
		case CodecVP9:
			skipCodecs = append(skipCodecs, transworker.CodecVP9)
		case CodecAV1:
			skipCodecs = append(skipCodecs, transworker.CodecAV1)
		}
	}
	return transworker.TransWorkerOptions{
		MaxConcurrency: c.MaxConcurrency,
		TargetQuality:  targetQuality,
		MaxResolution:  maxResolution,
		MaxFps:         c.MaxFps,
		SkipCodecs:     skipCodecs,
	}
}

const validExtensions = ".mp4 .mkv .avi .mov"

func dispatchAndWait(rootPath string, conf *CliConfig) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tw := transworker.NewFFMpegTransWorker(
		ctx,
		conf.asTWConfig(),
	)
	remainingCount := 0

	// debug print the config
	fmt.Printf("Config: %+v\n", conf)

	// walk the path
	wo := WalkerOption{
		MergeFiles: conf.MergeFiles,
		RootPath:   rootPath,
		OnGroupFound: func(files []string) error {
			filteredFiles := make([]string, 0, len(files))
			// we only accept video files, so we can filter the files by checking their extensions, this is a simple way to filter out non-video files and avoid unnecessary transcoding attempts.
			for _, file := range files {
				ext := path.Ext(file)
				if strings.Contains(validExtensions, ext) {
					filteredFiles = append(filteredFiles, file)
				}
			}
			if len(filteredFiles) == 0 {
				return nil
			}
			taskInput := transworker.TranscodingTaskInput{
				Files: filteredFiles,
			}
			needTranscode, err := tw.Dispatch(ctx, taskInput)
			if err != nil {
				if errors.Is(err, transworker.ErrNoFilesToTranscode) {
					return nil
				}
				return err
			}
			if needTranscode {
				remainingCount++
			}
			return nil
		},
	}
	err := wo.Walk()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking the path: %v\n", err)
		os.Exit(1)
	}
	p := mpb.New()
	rootName := path.Base(rootPath)
	bar := p.AddBar(int64(remainingCount),
		mpb.PrependDecorators(
			decor.Name(fmt.Sprintf("[%s]", rootName), decor.WC{W: len(rootName) + 2, C: decor.DindentRight}),
			decor.CountersNoUnit("%d / %d", decor.WCSyncSpace),
		),
		mpb.AppendDecorators(
			decor.Percentage(decor.WC{W: 5}),
			decor.OnComplete(
				decor.AverageETA(decor.ET_STYLE_GO), "done",
			),
		),
	)


	logger := log.New(p, "[LOG] ", log.LstdFlags)
	// print progress bar while wait for wg
	for range remainingCount {
		empied, result := tw.WaitTask(ctx)
		if empied {
			// no more task, just break the loop
			return
		}
		if result.Err != nil {
			for _, info := range result.OriginalFiles {
				logger.Printf("%s transcoding failed, codec: %s, resolution: %dx%d, fps: %.2f, bitrate: %d\n",
					path.Base(info.FilePath),
					info.Codec,
					info.Width,
					info.Height,
					info.Fps,
					info.Bitrate,
				)
			}
			logger.Printf("Error transcoding files: %v\n", result.Err)
			continue
		}
		// update mbp progress bar
		bar.Increment()
		// log the transcoding result
		for _, info := range result.TranscodedFiles {
			logger.Printf("%s finished, codec: %s, resolution: %dx%d, fps: %.2f, bitrate: %d\n",
				path.Base(info.FilePath),
				info.Codec,
				info.Width,
				info.Height,
				info.Fps,
				info.Bitrate,
			)
		}
	}
	p.Shutdown()
	p.Wait()
}
