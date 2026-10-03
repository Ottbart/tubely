package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
)

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	output := new(bytes.Buffer)
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run ffprobe on %q: %w", filePath, err)
	}

	var result struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		return "", fmt.Errorf("decode ffprobe output for %q: %w", filePath, err)
	}

	for _, stream := range result.Streams {
		if stream.CodecType != "video" {
			continue
		}
		width, height := stream.Width, stream.Height

		if width <= 0 || height <= 0 {
			return "", fmt.Errorf("ffprobe returned invalid video dimensions for %q", filePath)
		}
		ratio := float64(width) / float64(height)
		if ratio > 1.7 && ratio < 1.8 {
			return "16:9", nil
		}
		if ratio > 0.5 && ratio < 0.6 {
			return "9:16", nil
		}
		return "other", nil
	}
	return "", fmt.Errorf("ffprobe output for %q contains no video stream", filePath)
}

func processVideoForFastStart(filePath string) (string, error) {
	outputPath := filePath + ".processing"
	cmd := exec.Command(
		"ffmpeg",
		"-i", filePath,
		"-c",
		"copy",
		"-movflags",
		"faststart",
		"-f",
		"mp4",
		outputPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run ffmpeg on %q: %w", filePath, err)
	}

	return outputPath, nil
}
