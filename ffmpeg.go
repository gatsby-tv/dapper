package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path"
	"strconv"

	"github.com/google/uuid"
	"github.com/spf13/viper"
)

// HLSChunkLength - Size of HLS pieces in seconds
const HLSChunkLength = 10

func getVideoLength(videoFile string) (videoLength int, err error) {
	// Get length of video
	cmd := exec.Command(viper.GetString("ffprobeDir"), "-i", videoFile, "-show_entries", "format=duration", "-v", "quiet", "-of", `csv=p=0`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return 0, err
	}

	videoLengthFloat, err := strconv.ParseFloat(string(out[:len(out)-1]), 64)
	if err != nil {
		return 0, err
	}

	videoLength = int(math.Ceil(videoLengthFloat))

	return videoLength, nil
}

func convertToHLS(videoFile string) (videoFolder string, err error) {
	// Create folder to store HLS video in
	videoFolder = path.Join(viper.GetString("videoStorageFolder"), uuid.New().String())
	// TODO: Use proper FileMode
	err = os.Mkdir(videoFolder, 0755)
	if err != nil {
		return "", err
	}

	// Convert video
	// TODO: Create way of checking status of transcode
	cmd := exec.Command(viper.GetString("ffmpegDir"), "-i", videoFile, "-profile:v", "baseline", "-level", "3.0", "-s", "1920x1080", "-start_number", "0", "-hls_time", fmt.Sprint(HLSChunkLength), "-hls_list_size", "0", "-f", "hls", path.Join(videoFolder, "/master.m3u8"))

	fmt.Printf("Converting %s to HLS\n", videoFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return "", err
	}

	return videoFolder, nil
}
