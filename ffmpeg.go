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
	CID             string
}

var videoResolutions = []int{426, 640, 854, 1280, 1920}

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

// Gets the number of frames in the given video
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

func getVideoResolution(videoFile string) (string, error) {
	cmd := exec.Command(viper.GetString("ffmpeg.ffprobeDir"), "-i", videoFile, "-show_entries", "stream=width,height", "-v", "quiet", "-of", `csv=s=x:p=0`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.New(string(out) + " | " + err.Error())
	}

	return strings.TrimSpace(string(out)), nil
}

// Converts the given video to HLS chunks and places them in a folder named with the video's UUID
func convertToHLS(videoFile, videoUUID string) (videoFolder string, err error) {
	// Create folder to store HLS video in
	videoFolder = path.Join(viper.GetString("Videos.TempVideoStorageFolder"), videoUUID)
	err = os.Mkdir(videoFolder, 0755)
	if err != nil {
		return "", err
	}

	// Build ffmpeg command
	ffmpegArgs := []string{"-i", videoFile, "-loglevel", "error", "-progress", "-", "-nostats", "-filter_complex"}

	videoResolution, err := getVideoResolution(videoFile)
	if err != nil {
		return "", err
	}

	videoWidth, err := strconv.ParseInt(strings.Split(videoResolution, "x")[0], 10, 64)
	if err != nil {
		return "", err
	}
	videoHeight, err := strconv.ParseInt(strings.Split(videoResolution, "x")[1], 10, 64)
	if err != nil {
		return "", err
	}

	aspectRatio := float64(videoHeight) / float64(videoWidth)

	maxResolutionIndex := 0

	for ; videoWidth > int64(videoResolutions[maxResolutionIndex]); maxResolutionIndex++ {
	}

	if videoWidth == int64(videoResolutions[maxResolutionIndex]) {
		maxResolutionIndex++
	}

	outputResolutions := videoResolutions[0:maxResolutionIndex]
	numResolutions := len(outputResolutions)

	filterString := fmt.Sprintf("[0:v]split=%d", numResolutions)

	for i := 0; i < numResolutions; i++ {
		filterString += fmt.Sprintf("[v%d]", i+1)
	}

	filterString += "; "

	for i := 0; i < numResolutions; i++ {
		resolutionHeight := int(math.Floor(float64(videoResolutions[i]) * aspectRatio))
		if resolutionHeight%2 != 0 {
			resolutionHeight++
		}

		filterString += fmt.Sprintf("[v%d]scale=w=%d:h=%d[v%dout]", i+1, videoResolutions[i], resolutionHeight, i+1)

		if i < numResolutions-1 {
			filterString += "; "
		}
	}

	ffmpegArgs = append(ffmpegArgs, filterString)

	for i := 0; i < numResolutions; i++ {
		ffmpegArgs = append(ffmpegArgs, "-map", fmt.Sprintf("[v%dout]", i+1), "-map", "a:0", fmt.Sprintf("-c:a:%d", i), "aac")
	}

	ffmpegArgs = append(ffmpegArgs, "-var_stream_map")

	streamMap := ""
	for i := 0; i < numResolutions; i++ {
		streamMap += fmt.Sprintf("v:%d,a:%d", i, i)

		if i < numResolutions-1 {
			streamMap += " "
		}
	}

	ffmpegArgs = append(ffmpegArgs, streamMap, "-hls_playlist_type", "vod", "-hls_flags", "independent_segments", "-hls_segment_type", "mpegts", "-hls_segment_filename", path.Join(videoFolder, "stream_%v_data%02d.ts"), "-hls_time", "10", "-master_pl_name", "master.m3u8", "-f", "hls", path.Join(videoFolder, "stream_%v.m3u8"))

	// Convert video
	cmd := exec.Command(viper.GetString("ffmpeg.ffmpegDir"), ffmpegArgs...)

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

// Updates the encoding map with the progress of the video encode job
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

		// Take the frame count out of the output of ffmpeg
		output := string(buf)
		frameLine := strings.Split(output, "\n")[0]
		frameCountStr := strings.Split(frameLine, "=")[1]
		frameCount, _ := strconv.ParseInt(frameCountStr, 10, 64)

		encodingVideos.mutex.Lock()
		// Calculate the progress percentage
		encodingProgress := int64(math.Floor(float64(frameCount) * 100 / float64(encodingVideos.Videos[videoUUID].TotalFrames)))
		// Update the encoding map
		tempStruct := EncodingVideo{TotalFrames: encodingVideos.Videos[videoUUID].TotalFrames, CurrentProgress: encodingProgress}
		encodingVideos.Videos[videoUUID] = tempStruct
		encodingVideos.mutex.Unlock()
	}

	// When the stdout reader is closed, ffmpeg has finished
	// Update the encoding map to signal that the job has completed
	encodingVideos.mutex.Lock()
	tempStruct := EncodingVideo{TotalFrames: encodingVideos.Videos[videoUUID].TotalFrames, CurrentProgress: -1}
	encodingVideos.Videos[videoUUID] = tempStruct
	encodingVideos.mutex.Unlock()
}
