package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Run("DefaultValues", func(t *testing.T) {
		config, err := LoadConfig("")
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}

		if config.MergeFiles != false {
			t.Errorf("Expected default MergeFiles to be false, got %v", config.MergeFiles)
		}
		if config.MaxFps != 60 {
			t.Errorf("Expected default MaxFps to be 60, got %d", config.MaxFps)
		}
		if config.MaxResolution.Width != 3840 {
			t.Errorf("Expected default Width to be 3840, got %d", config.MaxResolution.Width)
		}
	})

	t.Run("EnvironmentVariables", func(t *testing.T) {
		os.Setenv("TRANSWORKER_MERGE_FILES", "true")
		os.Setenv("TRANSWORKER_MAX_FPS", "120")
		defer func() {
			os.Unsetenv("TRANSWORKER_MERGE_FILES")
			os.Unsetenv("TRANSWORKER_MAX_FPS")
		}()

		config, err := LoadConfig("")
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}

		if config.MergeFiles != true {
			t.Errorf("Expected MergeFiles to be true from env, got %v", config.MergeFiles)
		}
		if config.MaxFps != 120 {
			t.Errorf("Expected MaxFps to be 120 from env, got %d", config.MaxFps)
		}
	})

	t.Run("ConfigFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "test_config.yaml")
		yamlContent := `
merge_files: true
max_fps: 30
max_resolution:
  width: 1920
  height: 1080
skip_codecs: ["h264"]
`
		if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
			t.Fatalf("Failed to write temp config file: %v", err)
		}

		config, err := LoadConfig(configPath)
		if err != nil {
			t.Fatalf("Failed to load config from file: %v", err)
		}

		if config.MergeFiles != true {
			t.Error("MergeFiles should be true from file")
		}
		if config.MaxFps != 30 {
			t.Errorf("Expected MaxFps 30, got %d", config.MaxFps)
		}
		if config.MaxResolution.Height != 1080 {
			t.Errorf("Expected Height 1080, got %d", config.MaxResolution.Height)
		}
		if len(config.SkipCodecs) != 1 || config.SkipCodecs[0] != CodecH264 {
			t.Errorf("Expected skip_codecs [h264], got %v", config.SkipCodecs)
		}
	})

	t.Run("ExampleConfigFile", func(t *testing.T) {
		// Path to the example file in the project root
		examplePath := filepath.Join("..", "..", ".transworker.example.yaml")

		config, err := LoadConfig(examplePath)
		if err != nil {
			t.Fatalf("Failed to load example config: %v", err)
		}

		// Verify a few values from the example file
		if config.MaxFps != 60 {
			t.Errorf("Expected MaxFps 60 from example, got %d", config.MaxFps)
		}
		if len(config.SkipCodecs) != 3 {
			t.Errorf("Expected 3 skip_codecs from example, got %d", len(config.SkipCodecs))
		}
	})
}
