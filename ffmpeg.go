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

var videoResolutions = []string{"426x240", "640x360", "854x480", "1280x720", "1920x1080"}
var resolutionFfmpegParts = map[string]string{"426x240": "scale=w=426:h=240", "640x360": "scale=w=640:h=360", "854x480": "scale=w=854:h=480", "1280x720": "scale=w=1280:h=720", "1920x1080": "scale=w=1920:h=1080"}

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

	ffmpegArgs := []string{"-i", videoFile, "-loglevel", "error", "-progress", "-", "-nostats", "-filter_complex"}

	// Determine resolutions to output
	videoResolution, err := getVideoResolution(videoFile)
	if err != nil {
		return "", err
	}

	maxResolutionIndex := 0

	for ; videoResolution != videoResolutions[maxResolutionIndex]; maxResolutionIndex++ {
	}

	maxResolutionIndex++

	outputResolutions := videoResolutions[0:maxResolutionIndex]
	numResolutions := len(outputResolutions)

	filterString := fmt.Sprintf("[0:v]split=%d", numResolutions)

	for i := 0; i < numResolutions; i++ {
		filterString += fmt.Sprintf("[v%d]", i+1)
	}

	filterString += "; "

	for i := 0; i < numResolutions; i++ {
		filterString += fmt.Sprintf("[v%d]%s[v%dout]", i+1, resolutionFfmpegParts[outputResolutions[i]], i+1)

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

	fmt.Println(ffmpegArgs)
	// Convert video
	// Possible resolutions: 1920x1080, 1280x720, 854x480, 640x360, 426x240
	// ffmpeg -i /home/nesbitt/Videos/Raccoon.mp4 -loglevel error -progress - -nostats -filter_complex "[0:v]split=3[v1][v2][v3]; [v1]copy[v1out]; [v2]scale=w=1280:h=720[v2out]; [v3]scale=w=640:h=360[v3out]" -map "[v1out]" -map "[v2out]" -map "[v3out]" -map a:0 -c:a:0 aac -map a:0 -c:a:1 aac -map a:0 -c:a:2 aac -var_stream_map "v:0,a:0 v:1,a:1 v:2,a:2" -hls_playlist_type vod -hls_flags independent_segments -hls_segment_type mpegts -hls_segment_filename "stream_%v_data%02d.ts" -hls_time 10 -master_pl_name "master.m3u8" -f hls "stream_%v.m3u8"
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
