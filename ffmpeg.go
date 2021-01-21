package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/google/uuid"
	"github.com/spf13/viper"
)

// HLSChunkLength - Size of HLS pieces in seconds
const HLSChunkLength = 10

func convertToHLS(videoFile string) (string, error) {
	videoFolder := path.Join(viper.GetString("videoStorageFolder"), uuid.New().String())

	// TODO: Use proper FileMode
	err := os.Mkdir(videoFolder, 0755)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(viper.GetString("ffmpegDir"), "-i", videoFile, "-profile:v", "baseline", "-level", "3.0", "-s", "1920x1080", "-start_number", "0", "-hls_time", fmt.Sprint(HLSChunkLength), "-hls_list_size", "0", "-f", "hls", path.Join(videoFolder, "/master.m3u8"))

	fmt.Printf("Converting %s to HLS\n", videoFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return "", err
	}

	return videoFolder, nil
}
