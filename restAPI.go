package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

// Response given by dapper to a POST to "/video".
// Gives the caller the ID of the video within dapper to check its status and get the finished CID.
type VideoStartEncodingResponse struct {
	ID string `json:"id"`
}

// Response given by dapper to a GET to "/status".
// Gives the caller the status of a running video encoding job.
// If the job is complete, it returns the CID of the pinned video.
type VideoEncodingStatusResponse struct {
	Finished bool   `json:"finished"`
	Progress int64  `json:"progress"`
	CID      string `json:"cid"`
	Length   int    `json:"length"`
	Error    string `json:"error"`
}

// Response given by dapper to a POST to "/thumbnail".
// Gives the caller the CID of the thumbnail after it is added to IPFS.
type ThumbnailUploadResponse struct {
	CID string `json:"cid"`
}

// Maximum memory to attempt to store multipart form data in.
const multipartMaxMemory = 50 << 20 // 50MiB
// Folder name to store intermediate multipart form data in.
// This folder is placed in the temp video storage folder.
const videoScratchFolder = "scratch"

// Starts listening for requests on the given port
func handleRequests(port int) {
	e := echo.New()

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// GETs
	// e.GET("/traffic", getCurrentOutTraffic)
	e.GET("/status", encodingStatus)

	// POSTs
	e.POST("/video", uploadVideo)
	e.POST("/thumbnail", uploadThumbnail)

	e.Logger.Fatal(e.Start(fmt.Sprintf(":%d", port)))
}

// Routes

// GETs

// Returns the status of the encoding job or CID if it is completed
func encodingStatus(c echo.Context) error {
	var response error
	keys := c.QueryParam("id")

	// Check that the id param was given
	if len(keys) < 1 {
		return c.String(http.StatusBadRequest, "Param 'id' is missing")
	}

	encodingVideos.mutex.Lock()

	// Check that the video is in the encoding map
	if progress, ok := encodingVideos.Videos[keys]; ok {
		// Check if the encode has finished
		if progress.CurrentProgress == -1 {
			if progress.Error != nil {
				statusResponse := VideoEncodingStatusResponse{Finished: true, Error: progress.Error.Error()}
				response = c.JSON(http.StatusInternalServerError, statusResponse)
			} else {
				statusResponse := VideoEncodingStatusResponse{Finished: true, CID: progress.CID, Length: progress.Length}

				response = c.JSON(http.StatusCreated, statusResponse)
			}

			delete(encodingVideos.Videos, keys)
		} else {
			statusResponse := VideoEncodingStatusResponse{Finished: false, Progress: progress.CurrentProgress}

			response = c.JSON(http.StatusAccepted, statusResponse)
		}
	} else {
		response = c.String(http.StatusNotFound, "Specified ID is not transcoding.")
	}

	encodingVideos.mutex.Unlock()

	log.Trace().Msgf("Returning status for %s", keys[0])

	return response
}

// func getCurrentOutTraffic(w http.ResponseWriter, r *http.Request) {
// 	fmt.Fprintf(w, "%s/s", humanize.Bytes(uint64(Reporter.GetBandwidthTotals().RateOut)))
// }

// POSTs

// Take video and thumbnail from multipart form data, transfer it to the disk, convert it to HLS, then pin it with IPFS.
func uploadVideo(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form data
	err := r.ParseMultipartForm(multipartMaxMemory)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error parsing multipart form data: %s", err)
		return
	}

	// Check for the necessary files in the multipart form data
	video, videoHeader, err := r.FormFile("video")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Failed getting video from multipart form data: %s", err)
		return
	}
	defer video.Close()

	// Write video to disk
	videoUUID := uuid.New().String()

	videoFilename := path.Join(viper.GetString("Videos.TempVideoStorageFolder"), videoScratchFolder, videoUUID+"."+strings.Split(videoHeader.Filename, ".")[len(strings.Split(videoHeader.Filename, "."))-1])

	err = writeMultiPartFormDataToDisk(video, videoFilename)
	if err != nil {
		log.Error().Msgf("Failed writing video to disk: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Failed writing video to disk")
		return
	}

	json.NewEncoder(w).Encode(VideoStartEncodingResponse{ID: videoUUID})

	log.Trace().Msgf("Finished video pre-processing. Starting encoding of %s", videoFilename)

	// Run rest of video upload async
	go asyncVideoUpload(videoFilename, videoUUID)
}

func uploadThumbnail(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form data
	err := r.ParseMultipartForm(multipartMaxMemory)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error parsing multipart form data: %s", err)
		return
	}

	// Check for the necessary files in the multipart form data
	thumbnail, thumbnailHeader, err := r.FormFile("thumbnail")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Failed getting thumbnail from multipart form data: %s", err)
		return
	}
	defer thumbnail.Close()

	// Write thumbnail to disk
	thumbnailFilename := path.Join(viper.GetString("Videos.TempVideoStorageFolder"), videoScratchFolder, uuid.New().String()+"-thumbnail"+"."+strings.Split(thumbnailHeader.Filename, ".")[len(strings.Split(thumbnailHeader.Filename, "."))-1])

	err = writeMultiPartFormDataToDisk(thumbnail, thumbnailFilename)
	if err != nil {
		log.Error().Msgf("Failed writing thumbnail to disk: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Failed writing thumbnail to disk")
		return
	}

	thumbnailCID, err := addFileToIPFS(r.Context(), thumbnailFilename)
	if err != nil {
		log.Error().Msgf("Failed adding thumbnail to IPFS: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Failed adding thumbnail to IPFS")
		return
	}

	// Remove scratch thumbnail file
	os.Remove(thumbnailFilename)

	json.NewEncoder(w).Encode(ThumbnailUploadResponse{CID: thumbnailCID})
}

// Private Functions

// Transcode and pin video asynchronously while dapper continues to listen for requests
func asyncVideoUpload(video, videoUUID string) {
	ctx := context.Background()
	defer ctx.Done()

	// Create entry for video in the global map
	encodingVideos.mutex.Lock()
	encodingVideos.Videos[videoUUID] = EncodingVideo{TotalFrames: 1, CurrentProgress: 0}
	encodingVideos.mutex.Unlock()

	// Get the length of the video in seconds
	videoLength, err := getVideoLength(video)
	if err != nil {
		log.Error().Msgf("Unable to get video length: %s\n", err)
		encodingVideos.mutex.Lock()
		encodingVideos.Videos[videoUUID] = EncodingVideo{Error: err, CurrentProgress: -1}
		encodingVideos.mutex.Unlock()
		return
	}

	// Get the number of frames in the video for tracking encoding progress
	videoFrames, err := getVideoFrames(video, videoLength)
	if err != nil {
		log.Error().Msgf("Unable to count video frames: %s\n", err)
		encodingVideos.mutex.Lock()
		encodingVideos.Videos[videoUUID] = EncodingVideo{Error: err, CurrentProgress: -1}
		encodingVideos.mutex.Unlock()
		return
	}

	// Update the global map with the total number of frames in the current video
	encodingVideos.mutex.Lock()
	encodingVideos.Videos[videoUUID] = EncodingVideo{TotalFrames: videoFrames, CurrentProgress: 0}
	encodingVideos.mutex.Unlock()

	// Convert video to HLS pieces
	videoFolder, err := convertToHLS(video, videoUUID)
	if err != nil {
		log.Error().Msgf("Unable to convert video to HLS: %s\n", err)
		encodingVideos.mutex.Lock()
		encodingVideos.Videos[videoUUID] = EncodingVideo{Error: err, CurrentProgress: -1}
		encodingVideos.mutex.Unlock()
		return
	}

	// Remove scratch video file
	os.Remove(video)

	// Add video folder to IPFS
	videoCID, err := addFolderToIPFS(ctx, videoFolder)
	if err != nil {
		log.Error().Msgf("Unable to add video folder to IPFS: %s\n", err)
		encodingVideos.mutex.Lock()
		encodingVideos.Videos[videoUUID] = EncodingVideo{Error: err, CurrentProgress: -1}
		encodingVideos.mutex.Unlock()
		return
	}
	log.Info().Msgf("Video folder added to IPFS: %s\n", videoCID)

	// Remove converted video folder
	err = os.RemoveAll(videoFolder)
	if err != nil {
		log.Error().Msgf("Failed removing video folder: %s\n", err)
	}

	// Update the map with the video CID
	encodingVideos.mutex.Lock()
	tempStruct := EncodingVideo{CID: videoCID, CurrentProgress: -1, Length: videoLength}
	encodingVideos.Videos[videoUUID] = tempStruct
	encodingVideos.mutex.Unlock()

	log.Info().Msgf("Finished transcoding %s.\n", video)
}

// Writes given multipart form data object to the file specified
func writeMultiPartFormDataToDisk(multipartFormData io.ReadCloser, destFile string) error {
	tempFile, err := os.Create(destFile)
	if err != nil {
		return err
	}
	defer tempFile.Close()

	io.Copy(tempFile, multipartFormData)

	return nil
}
