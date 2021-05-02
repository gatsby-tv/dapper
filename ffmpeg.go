package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

// HLSChunkLength - Size of HLS pieces in seconds
const HLSChunkLength = 10

type EncodingVideos struct {
	mutex  sync.Mutex
	Videos map[string]EncodingVideo
}

type EncodingVideo struct {
	TotalFrames     int64
	CurrentProgress int64
}

var encodingVideos EncodingVideos

// Uses `ffprobe` to find the length of the video in seconds (ceilinged ot next largest int)
func getVideoLength(videoFile string) (videoLength int, err error) {
	cmd := exec.Command(viper.GetString("ffmpeg.ffprobeDir"), "-i", videoFile, "-show_entries", "format=duration", "-v", "quiet", "-of", `csv=p=0`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, errors.New(string(out) + " | " + err.Error())
	}

	videoLengthFloat, err := strconv.ParseFloat(string(out[:len(out)-1]), 64)
	if err != nil {
		return 0, err
	}

	videoLength = int(math.Ceil(videoLengthFloat))

	return videoLength, nil
}

func getVideoFrames(videoFile string, videoLength int) (int64, error) {
	cmd := exec.Command(viper.GetString("ffmpeg.ffprobeDir"), "-i", videoFile, "-show_entries", "stream=r_frame_rate", "-v", "error", "-of", `default=nokey=1:noprint_wrappers=1`, "-select_streams", "v:0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, errors.New(string(out) + " | " + err.Error())
	}

	output := string(out)
	videoFPS, err := strconv.ParseInt(strings.Split(output, "/")[0], 10, 64)
	if err != nil {
		return 0, err
	}

	return int64(videoLength) * videoFPS, nil
}

// Converts the given video to HLS chunks and places them in a folder named with the video's UUID
func convertToHLS(videoFile, videoUUID string) (videoFolder string, err error) {
	// Create folder to store HLS video in
	videoFolder = path.Join(viper.GetString("Videos.TempVideoStorageFolder"), videoUUID)
	err = os.Mkdir(videoFolder, 0755)
	if err != nil {
		return "", err
	}

	// Convert video
	cmd := exec.Command(viper.GetString("ffmpeg.ffmpegDir"), "-i", videoFile, "-loglevel", "error", "-progress", "-", "-nostats", "-profile:v", "baseline", "-level", "3.0", "-s", "1920x1080", "-start_number", "0", "-hls_time", fmt.Sprint(HLSChunkLength), "-hls_list_size", "0", "-f", "hls", path.Join(videoFolder, "/master.m3u8"))

	fmt.Printf("Converting %s to HLS\n", videoFile)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	err = cmd.Start()
	if err != nil {
		return "", err
	}

	go updateEncodeFrameProgress(stdout, videoUUID)

	err = cmd.Wait()
	if err != nil {
		return "", err
	}

	return videoFolder, nil
}

func updateEncodeFrameProgress(ffmpegStdOut io.ReadCloser, videoUUID string) {
	buf := make([]byte, 1<<10)
	for endOfFile := false; !endOfFile; {
		_, err := ffmpegStdOut.Read(buf)
		if err == io.EOF {
			endOfFile = true
			continue
		} else if err != nil {
			fmt.Printf("Error updating video progress: %s\n", err)
			endOfFile = true
			continue
		}

		output := string(buf)

		frameLine := strings.Split(output, "\n")[0]
		frameCountStr := strings.Split(frameLine, "=")[1]

		frameCount, _ := strconv.ParseInt(frameCountStr, 10, 64)

		encodingVideos.mutex.Lock()

		encodingProgress := int64(math.Floor(float64(frameCount) * 100 / float64(encodingVideos.Videos[videoUUID].TotalFrames)))

		tempStruct := EncodingVideo{TotalFrames: encodingVideos.Videos[videoUUID].TotalFrames, CurrentProgress: encodingProgress}
		encodingVideos.Videos[videoUUID] = tempStruct

		encodingVideos.mutex.Unlock()
	}

	encodingVideos.mutex.Lock()
	delete(encodingVideos.Videos, videoUUID)
	encodingVideos.mutex.Unlock()
}
