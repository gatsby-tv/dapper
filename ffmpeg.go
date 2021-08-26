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
var standardVideoHeigths = []int64{240, 360, 480, 720, 1080}

// These are the standard heights for 16:9 videos
var videoResolutionsStr = []string{"240", "360", "480", "720", "1080"}

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

/*
ffmpeg -i f7b50a9b-d3a1-4959-aa40-b95a6f879803.webm -filter_complex '[0:v]split=3[v1][v2][v3]; [v1]copy[v1out]; [v2]scale=w=1280:h=720[v2out]; [v3]scale=w=640:h=360[v3out]' -map '[v1out]' -c:v:0 libx265 -b:v:0 5M -maxrate:v:0 5M -minrate:v:0 5M -bufsize:v:0 10M -preset slow -g 48 -sc_threshold 0 -keyint_min 48 -map '[v2out]' -c:v:1 libx265 -b:v:1 3M -maxrate:v:1 3M -minrate:v:1 3M -bufsize:v:1 3M -preset slow -g 48 -sc_threshold 0 -keyint_min 48 -map '[v3out]' -c:v:2 libx265 -b:v:2 1M -maxrate:v:2 1M -minrate:v:2 1M -bufsize:v:2 1M -preset slow -g 48 -sc_threshold 0 -keyint_min 48 -map a:0 -c:a:0 aac -b:a:0 96k -ac 2 -map a:0 -c:a:1 aac -b:a:1 96k -ac 2 -map a:0 -c:a:2 aac -b:a:2 48k -ac 2 -f hls -hls_time 2 -hls_playlist_type vod -hls_flags independent_segments -hls_segment_type mpegts -hls_segment_filename stream_%v-data%02d.ts -master_pl_name master.m3u8 -var_stream_map "v:0,a:0 v:1,a:1 v:2,a:2" stream_%v.m3u8
*/

func buildFfmpegFilter(videoFile string, numResolutions int) ([]string, error) {
	ffmpegFilter := []string{"-filter_complex"}
	filterString := fmt.Sprintf("[0:v]split=%d", numResolutions)

	// Split the video into numResolutions parts
	for i := 0; i < numResolutions; i++ {
		filterString += fmt.Sprintf("[v%d]", i+1)
	}

	filterString += "; "

	// Scale each stream to the appropriate resolution
	for i := 0; i < numResolutions; i++ {
		filterString += fmt.Sprintf("[v%d]scale=w=%d:h=%d:force_original_aspect_ratio=decrease[v%dout]", i+1, standardVideoWidths[i], standardVideoHeigths[i], i+1)
		if (i + 1) < numResolutions {
			filterString += "; "
		}
	}

	ffmpegFilter = append(ffmpegFilter, filterString)

	return ffmpegFilter, nil
}

func buildFfmpegVideoStreamParams(videoFile string, numResolutions int) ([]string, error) {
	ffmpegVideoStreamParams := []string{"-map"}
	streamMap := fmt.Sprintf("[v%dout]", numResolutions)
	for i := 0; i < numResolutions; i++ {
		streamMap += fmt.Sprintf(",a:%d", i)
	}
	ffmpegVideoStreamParams = append(ffmpegVideoStreamParams, streamMap)
	return ffmpegVideoStreamParams, nil
}

// Builds the array of arguments necessary for ffmpeg to properly transcode the given video
// This downscales the video to the maximum and below of a horizontal width of 1920, 1280, 854, 640, 426
func buildFfmpegCommand(videoFile, videoFolder string) ([]string, error) {
	// Initial arguments for formatting ffmpeg's output
	ffmpegArgs := []string{"-i", videoFile, "-loglevel", "error", "-progress", "-", "-nostats"}

	maxResolutionIndex, err := determineMaxResolutionIndex(videoFile, videoFolder)
	if err != nil {
		return nil, err
	}

	outputResolutions := standardVideoWidths[0:maxResolutionIndex]
	numResolutions := len(outputResolutions)

	// Add a series of parameters for each resolution
	for i := 0; i < numResolutions; i++ {
		ffmpegArgs = append(ffmpegArgs, "-s", strconv.FormatInt(standardVideoWidths[i], 10)+"x"+strconv.FormatInt(resolutionHeight, 10), "-hls_playlist_type", "vod", "-hls_flags", "independent_segments", "-hls_segment_type", "mpegts", "-hls_segment_filename", path.Join(videoFolder, "stream_"+videoResolutionsStr[i]+"_data%02d.ts"), "-hls_time", "10", "-master_pl_name", "master"+videoResolutionsStr[i]+".m3u8", "-f", "hls", path.Join(videoFolder, "stream_"+videoResolutionsStr[i]+".m3u8"))
	}

	return ffmpegArgs, nil
}

func determineMaxResolutionIndex(videoFile, videoFolder string) (int, error) {
	// Get the resolution of the current video
	videoResolution, err := getVideoResolution(videoFile)
	if err != nil {
		return 0, err
	}

	videoWidth, err := strconv.ParseInt(strings.Split(videoResolution, "x")[0], 10, 64)
	if err != nil {
		return 0, err
	}

	// Find the maximum resolution to scale the video to
	maxResolutionIndex := 0
	for ; maxResolutionIndex < len(standardVideoWidths)-1 && videoWidth > int64(standardVideoWidths[maxResolutionIndex]); maxResolutionIndex++ {
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
		output := string(buf)
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
