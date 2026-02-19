package main

import "github.com/spf13/viper"

type TargetQualityLevel uint

const (
	TargetQualityLevelLow TargetQualityLevel = iota
	TargetQualityLevelMedium
	TargetQualityLevelHigh
)

type MaxResolution struct {
	Width  uint
	Height uint
}

type Codec string

const (
	CodecAV1  Codec = "av1"
	CodecH264 Codec = "h264"
	CodecH265 Codec = "h265"
	CodecVP9  Codec = "vp9"
)

type CliConfig struct {
	MergeFiles     bool               `mapstructure:"merge_files"`     // if true, first-level directories will be treated as groups, otherwise each file is a group
	TargetQuality  TargetQualityLevel `mapstructure:"target_quality"`  // target quality level for encoding, default is medium.
	MaxResolution  MaxResolution      `mapstructure:"max_resolution"`  // maximum resolution for encoding, default is 3840x3840 (4K portrait or landscape)
	MaxFps         uint               `mapstructure:"max_fps"`         // maximum frames per second for encoding, default is 60
	MaxConcurrency int                `mapstructure:"max_concurrency"` // maximum number of concurrent encoding tasks, default is 2 for general GPU encoding pipelines.
	SkipCodecs     []Codec            `mapstructure:"skip_codecs"`     // list of codecs to skip during encoding, e.g. ["h264", "vp9"], merge might still happen based on directories, but encoding will be skipped for files with these codecs.
}

func LoadConfig(configPath string) (*CliConfig, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("merge_files", false)
	v.SetDefault("target_quality", TargetQualityLevelMedium)
	v.SetDefault("max_resolution.width", 3840)
	v.SetDefault("max_resolution.height", 3840)
	v.SetDefault("max_fps", 60)
	v.SetDefault("max_concurrency", 2)
	v.SetDefault("skip_codecs", []Codec{CodecAV1, CodecH265, CodecVP9})

	// Environment variables
	v.SetEnvPrefix("TRANSWORKER")
	v.AutomaticEnv()

	// Config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName(".transworker")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.transworker")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok && configPath != "" {
			return nil, err
		}
	}

	var config CliConfig
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
