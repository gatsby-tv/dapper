package main

import (
	"bufio"
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

	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

// Data structure containing the vidoes currently being encoded/processed
type EncodingVideos struct {
	mutex  sync.Mutex
	Videos map[string]EncodingVideo
}

// Information about a video currently being encoded/processed
type EncodingVideo struct {
	TotalFrames     int64
	CurrentProgress int64
	CID             string
	Length          int
}

// HLSChunkLength - Size of HLS pieces in seconds
const HLSChunkLength = 10

// Standard video resolutions for transcoding videos
// These are the widths associated with the standard 16:9 resolutions
var standardVideoWidths = []int64{426, 640, 854, 1280, 1920}
var standardVideoHeights = []int64{240, 360, 480, 720, 1080}
var resolutionBitRates = map[int]string{
	240:  "500k",
	360:  "1M",
	480:  "2M",
	720:  "3M",
	1080: "5M",
}
var resolutionBufferSizes = map[int]string{
	240:  "1M",
	360:  "2M",
	480:  "4M",
	720:  "6M",
	1080: "10M",
}

// Videos Currently being processed
var encodingVideos EncodingVideos

// Uses `ffprobe` to find the length of the video in seconds (ceilinged to next largest int)
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

// FFMPEG command building

func buildFfmpegFilter(numResolutions int) []string {
	ffmpegFilter := []string{"-filter_complex"}
	filterString := fmt.Sprintf("[0:v]split=%d", numResolutions)

	// Split the video into numResolutions parts
	for i := 0; i < numResolutions; i++ {
		filterString += fmt.Sprintf("[v%d]", i+1)
	}

	filterString += "; "

	// Scale each stream to the appropriate resolution
	for i := 0; i < numResolutions; i++ {
		filterString += fmt.Sprintf("[v%d]scale=h=%d:w=%d:force_original_aspect_ratio=increase:force_divisible_by=2[v%dout]", i+1, standardVideoHeights[i], standardVideoWidths[i], i+1)
		if (i + 1) < numResolutions {
			filterString += "; "
		}
	}

	ffmpegFilter = append(ffmpegFilter, filterString)

	return ffmpegFilter
}

func buildFfmpegVideoStreamParams(numResolutions int) []string {
	ffmpegVideoStreamParams := []string{}

	for i := 0; i < numResolutions; i++ {
		ffmpegVideoStreamParams = append(ffmpegVideoStreamParams, "-map", fmt.Sprintf("[v%dout]", i+1), fmt.Sprintf("-c:v:%d", i), "libx264", fmt.Sprintf("-b:v:%d", i), resolutionBitRates[int(standardVideoHeights[i])], fmt.Sprintf("-maxrate:v:%d", i), resolutionBitRates[int(standardVideoHeights[i])], fmt.Sprintf("-minrate:v:%d", i), resolutionBitRates[int(standardVideoHeights[i])], fmt.Sprintf("-bufsize:v:%d", i), resolutionBufferSizes[int(standardVideoHeights[i])], "-preset", "fast", "-crf", "20", "-g", "48", "-sc_threshold", "0", "-keyint_min", "48")
	}

	return ffmpegVideoStreamParams
}

// -map a:0 -c:a:0 aac -b:a:0 96k -ac 2
func buildFfmpegAudioStreamParams(numResolutions int) []string {
	ffmpegAudioStreamParams := []string{}

	for i := 0; i < numResolutions; i++ {
		ffmpegAudioStreamParams = append(ffmpegAudioStreamParams, "-map", "a:0", fmt.Sprintf("-c:a:%d", i), "aac" /*fmt.Sprintf("-b:a:%d", i), "96k",*/, "-ac", "2")
	}

	return ffmpegAudioStreamParams
}

func buildFfmpegHLSParams(videoFolder string) []string {
	ffmpegHLSParams := []string{"-f", "hls", "-hls_time", "2", "-hls_playlist_type", "vod", "-hls_flags", "independent_segments", "-hls_segment_type", "mpegts", "-hls_segment_filename", path.Join(videoFolder, "stream_%v-data%02d.ts"), "-master_pl_name", "master.m3u8"}
	return ffmpegHLSParams
}

func buildFfmpegVarStreamMapParams(numResolutions int) []string {
	ffmpegVarStreamMapParams := []string{"-var_stream_map"}
	streamMap := ""

	for i := 0; i < numResolutions; i++ {
		streamMap += fmt.Sprintf("v:%d,a:%d", i, i)
		if (i + 1) < numResolutions {
			streamMap += " "
		}
	}

	ffmpegVarStreamMapParams = append(ffmpegVarStreamMapParams, streamMap)

	return ffmpegVarStreamMapParams
}

// Builds the array of arguments necessary for ffmpeg to properly transcode the given video
func buildFfmpegCommand(videoFile, videoFolder string) ([]string, error) {
	// Initial arguments for formatting ffmpeg's output
	ffmpegArgs := []string{"-i", videoFile, "-loglevel", "error", "-progress", "-", "-nostats"}

	maxResolutionIndex, err := determineMaxResolutionIndex(videoFile, videoFolder)
	if err != nil {
		return nil, err
	}

	outputResolutions := standardVideoHeights[0 : maxResolutionIndex+1]
	numResolutions := len(outputResolutions)

	ffmpegArgs = append(ffmpegArgs, buildFfmpegFilter(numResolutions)...)
	ffmpegArgs = append(ffmpegArgs, buildFfmpegVideoStreamParams(numResolutions)...)
	ffmpegArgs = append(ffmpegArgs, buildFfmpegAudioStreamParams(numResolutions)...)
	ffmpegArgs = append(ffmpegArgs, buildFfmpegHLSParams(videoFolder)...)
	ffmpegArgs = append(ffmpegArgs, buildFfmpegVarStreamMapParams(numResolutions)...)
	ffmpegArgs = append(ffmpegArgs, path.Join(videoFolder, "stream_%v.m3u8"))

	return ffmpegArgs, nil
}

func determineMaxResolutionIndex(videoFile, videoFolder string) (int, error) {
	// Get the resolution of the current video
	videoResolution, err := getVideoResolution(videoFile)
	if err != nil {
		return 0, err
	}

	videoHeight, err := strconv.ParseInt(strings.Split(videoResolution, "x")[1], 10, 64)
	if err != nil {
		return 0, err
	}

	// Find the maximum resolution to scale the video to
	maxResolutionIndex := len(standardVideoHeights) - 1
	for ; maxResolutionIndex > 0 && videoHeight < int64(standardVideoHeights[maxResolutionIndex]); maxResolutionIndex-- {
	}

	return maxResolutionIndex, nil
}

// Converts the given video to HLS chunks and places them in a folder named with the video's UUID
func convertToHLS(videoFile, videoUUID string) (videoFolder string, err error) {
	// Create folder to store HLS video in
	videoFolder = path.Join(viper.GetString("Videos.TempVideoStorageFolder"), videoUUID)
	err = os.Mkdir(videoFolder, 0755)
	if err != nil {
		return "", err
	}

	// Build the ffmpeg command that transcodes the given video to multiple HLS streams of different resolutions
	ffmpegArgs, err := buildFfmpegCommand(videoFile, videoFolder)
	if err != nil {
		return "", errors.New("Failed to build ffmpeg command: " + err.Error())
	}
	log.Debug().Msg(strings.Join(ffmpegArgs, " "))

	// Convert video
	cmd := exec.Command(viper.GetString("ffmpeg.ffmpegDir"), ffmpegArgs...)

	log.Info().Msgf("Converting %s to HLS...\n", videoFile)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	err = cmd.Start()
	if err != nil {
		return "", err
	}

	// Create a listener for ffmpeg's output to update `encodingVideos`
	go updateEncodeFrameProgress(stdout, videoUUID)
	go logStdErr(stderr)

	err = cmd.Wait()
	if err != nil {
		return "", err
	}

	return videoFolder, nil
}

func logStdErr(ffmpegStdErr io.ReadCloser) {
	scanner := bufio.NewScanner(ffmpegStdErr)
	for scanner.Scan() {
		log.Error().Str("ffmpeg", "stderr").Msg(scanner.Text())
	}
}

// Updates the encoding map with the progress of the video encode job
func updateEncodeFrameProgress(ffmpegStdOut io.ReadCloser, videoUUID string) {
	// Read until the end of the ffmpeg command
	buf := make([]byte, 1<<10)
	for endOfFile := false; !endOfFile; {
		_, err := ffmpegStdOut.Read(buf)
		if err == io.EOF {
			endOfFile = true
			continue
		} else if err != nil {
			log.Error().Msgf("Error updating video progress: %s\n", err)
			endOfFile = true
			continue
		}

		// Take the frame count out of the output of ffmpeg
		output := strings.Trim(string(buf), "\u0000")
		log.Debug().Str("ffmpeg", "stdout").Msg(output)
		frameLine := strings.Split(output, "\n")[0]
		frameCountStr := strings.Split(frameLine, "=")[1]
		frameCount, err := strconv.ParseInt(frameCountStr, 10, 64)
		if err != nil {
			log.Error().Msgf("Error updating video progress: %s\n", err)
			endOfFile = true
			continue
		}

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
	tempStruct := EncodingVideo{TotalFrames: encodingVideos.Videos[videoUUID].TotalFrames, CurrentProgress: 100}
	encodingVideos.Videos[videoUUID] = tempStruct
	encodingVideos.mutex.Unlock()
}
