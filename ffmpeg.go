package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
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

// Standard video resolutions for transcoding videos
// These are the widths associated with the standard 16:9 resolutions
var videoResolutions = []int64{426, 640, 854, 1280, 1920}

// These are the standard heights for 16:9 videos
var videoResolutionsStr = []string{"240", "360", "480", "720", "1080"}

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

// Uses ffmpeg to get the height and width of given video (as a string)
func getVideoResolution(videoFile string) (string, error) {
	cmd := exec.Command(viper.GetString("ffmpeg.ffprobeDir"), "-i", videoFile, "-show_entries", "stream=width,height", "-v", "quiet", "-of", `csv=s=x:p=0`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.New(string(out) + " | " + err.Error())
	}

	return strings.TrimSpace(string(out)), nil
}

// Builds the array of arguments necessary for ffmpeg to properly transcode the given video
// This downscales the video to the maximum and below of a horizontal width of 1920, 1280, 854, 640, 426
func buildFfmpegCommand(videoFile, videoFolder string) ([]string, int, error) {
	ffmpegArgs := []string{"-i", videoFile, "-loglevel", "error", "-progress", "-", "-nostats"}

	videoResolution, err := getVideoResolution(videoFile)
	if err != nil {
		return nil, 0, err
	}

	videoWidth, err := strconv.ParseInt(strings.Split(videoResolution, "x")[0], 10, 64)
	if err != nil {
		return nil, 0, err
	}
	videoHeight, err := strconv.ParseInt(strings.Split(videoResolution, "x")[1], 10, 64)
	if err != nil {
		return nil, 0, err
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

	for i := 0; i < numResolutions; i++ {
		resolutionHeight := int64(math.Floor(float64(videoResolutions[i]) * aspectRatio))
		if resolutionHeight%2 != 0 {
			resolutionHeight++
		}

		ffmpegArgs = append(ffmpegArgs, "-s", strconv.FormatInt(videoResolutions[i], 10)+"x"+strconv.FormatInt(resolutionHeight, 10), "-hls_playlist_type", "vod", "-hls_flags", "independent_segments", "-hls_segment_type", "mpegts", "-hls_segment_filename", path.Join(videoFolder, "stream_"+videoResolutionsStr[i]+"_data%02d.ts"), "-hls_time", "10", "-master_pl_name", "master"+videoResolutionsStr[i]+".m3u8", "-f", "hls", path.Join(videoFolder, "stream_"+videoResolutionsStr[i]+".m3u8"))
	}

	return ffmpegArgs, maxResolutionIndex, nil
}

// Combines the master playlists generated by ffmpeg into a single master playlist
func combineMasterPlaylists(videoFolder string, maxResolutionIndex int) error {
	destination, err := os.Create(path.Join(videoFolder, "master.m3u8"))
	if err != nil {
		return err
	}
	defer destination.Close()

	for i := 0; i < maxResolutionIndex; i++ {
		file, err := ioutil.ReadFile(path.Join(videoFolder, "master"+videoResolutionsStr[i]+".m3u8"))
		if err != nil {
			return err
		}

		// Write the entire first file to the master to get the necesssary headers
		if i == 0 {
			destination.Write(file)
		} else {
			lines := strings.Split(string(file), "\n")

			// Currently the way ffmpeg creates these files, the third and fourth lines contain the data specific to the stream in question
			destination.WriteString(lines[2])
			destination.WriteString("\n")
			destination.WriteString(lines[3])
			destination.WriteString("\n")
		}

		// Delete the file after it has been added to the master
		os.Remove(path.Join(videoFolder, "master"+videoResolutionsStr[i]+".m3u8"))

		destination.WriteString("\n")
	}

	return nil
}

// Converts the given video to HLS chunks and places them in a folder named with the video's UUID
func convertToHLS(videoFile, videoUUID string) (videoFolder string, err error) {
	// Create folder to store HLS video in
	videoFolder = path.Join(viper.GetString("Videos.TempVideoStorageFolder"), videoUUID)
	err = os.Mkdir(videoFolder, 0755)
	if err != nil {
		return "", err
	}

	ffmpegArgs, maxResolutionIndex, err := buildFfmpegCommand(videoFile, videoFolder)
	if err != nil {
		return "", errors.New("Failed to build ffmpeg command: " + err.Error())
	}

	// Convert video
	cmd := exec.Command(viper.GetString("ffmpeg.ffmpegDir"), ffmpegArgs...)

	fmt.Printf("Converting %s to HLS...\n", videoFile)
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

	err = combineMasterPlaylists(videoFolder, maxResolutionIndex)
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
