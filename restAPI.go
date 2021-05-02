package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/spf13/viper"
)

type VideoStartEncodingResponse struct {
	ID string `json:"id"`
}

type VideoEncodingStatusResponse struct {
	Progress int64 `json:"progress"`
}

const multipartMaxMemory = 50 << 20 // 50MiB
const videoScratchFolder = "scratch"

func handleRequests(port int) {
	myRouter := mux.NewRouter().StrictSlash(true)

	// GETs
	// myRouter.HandleFunc("/traffic", getCurrentOutTraffic).Methods("GET")
	myRouter.HandleFunc("/status", encodingStatus)

	// POSTs
	myRouter.HandleFunc("/video", uploadVideo).Methods("POST")

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), myRouter))
}

// func getCurrentOutTraffic(w http.ResponseWriter, r *http.Request) {
// 	fmt.Fprintf(w, "%s/s", humanize.Bytes(uint64(Reporter.GetBandwidthTotals().RateOut)))
// }

// Take video and thumbnail from multipart form data, transfer it to the disk, convert it to HLS, then pin it with IPFS.
func uploadVideo(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form data
	err := r.ParseMultipartForm(multipartMaxMemory)
	if err != nil {
		fmt.Fprintf(w, "Error parsing multipart form data: %s", err)
	}

	video, videoHeader, err := r.FormFile("video")
	if err != nil {
		fmt.Fprintf(w, "Failed getting video from multipart form data: %s", err)
	}

	thumbnail, thumbnailHeader, err := r.FormFile("thumbnail")

	// Write video to disk
	videoUUID := uuid.New().String()

	videoResponse := VideoStartEncodingResponse{ID: videoUUID}

	videoFilename := path.Join(viper.GetString("Videos.TempVideoStorageFolder"), videoScratchFolder, videoUUID+"."+strings.Split(videoHeader.Filename, ".")[len(strings.Split(videoHeader.Filename, "."))-1])

	tempFile, err := os.Create(videoFilename)
	if err != nil {
		fmt.Fprintf(w, "Failed opening video file for writing: %s", err)
	}

	// Buffer of 1MiB for transferring the file to disk
	buf := make([]byte, 1<<20)
	for endOfFile := false; !endOfFile; {
		_, err := video.Read(buf)
		if err == io.EOF {
			endOfFile = true
			continue
		} else if err != nil {
			fmt.Fprintf(w, "Error reading video from multipart form data: %s", err)
		}

		_, err = tempFile.Write(buf)
		if err != nil {
			fmt.Fprintf(w, "Error writing video to disk: %s", err)
		}
	}

	video.Close()
	tempFile.Close()

	// Write thumbnail to disk
	thumbnailFilename := path.Join(viper.GetString("Videos.TempVideoStorageFolder"), videoScratchFolder, videoUUID+"-thumbnail"+"."+strings.Split(thumbnailHeader.Filename, ".")[len(strings.Split(thumbnailHeader.Filename, "."))-1])

	tempFile, err = os.Create(thumbnailFilename)
	if err != nil {
		fmt.Fprintf(w, "Failed opening thumbnail file for writing: %s", err)
	}

	for endOfFile := false; !endOfFile; {
		_, err := thumbnail.Read(buf)
		if err == io.EOF {
			endOfFile = true
			continue
		} else if err != nil {
			fmt.Fprintf(w, "Error reading video from multipart form data: %s", err)
		}

		_, err = tempFile.Write(buf)
		if err != nil {
			fmt.Fprintf(w, "Error writing video to disk: %s", err)
		}
	}

	thumbnail.Close()
	tempFile.Close()

	json.NewEncoder(w).Encode(videoResponse)

	// Run rest of video upload async
	go asyncVideoUpload(videoFilename, thumbnailFilename, videoUUID)
}

// Transcode and pin video asynchronously while dapper continues to listen for requests
func asyncVideoUpload(video, thumbnail, videoUUID string) {
	ctx := context.Background()
	defer ctx.Done()

	encodingVideos.Videos[videoUUID] = EncodingVideo{TotalFrames: 1, CurrentProgress: 0}

	// Convert video to HLS pieces
	videoLength, err := getVideoLength(video)
	if err != nil {
		fmt.Printf("Unable to get video length: %s\n", err)
		return
	}

	videoFrames, err := getVideoFrames(video, videoLength)
	if err != nil {
		fmt.Printf("Unable to count video frames: %s\n", err)
		return
	}

	encodingVideos.Videos[videoUUID] = EncodingVideo{TotalFrames: videoFrames, CurrentProgress: 0}

	videoFolder, err := convertToHLS(video, videoUUID)
	if err != nil {
		fmt.Printf("Unable to convert video to HLS: %s\n", err)
		return
	}

	// Remove scratch video file
	os.Remove(video)

	thumbnailFileExtension := filepath.Ext(thumbnail)
	if err = fileCopy(thumbnail, path.Join(videoFolder, "thumbnail"+thumbnailFileExtension)); err != nil {
		fmt.Printf("Unable to copy thumbnail file: %s\n", err)
		return
	}

	// Add video to IPFS
	videoCID, err := addFolderToIPFS(ctx, videoFolder)
	if err != nil {
		fmt.Printf("Unable to add video folder to IPFS: %s\n", err)
		return
	}

	// Remove scratch thumbnail file
	os.Remove(thumbnail)

	thumbnailLocation := videoCID + "/thumbnail" + thumbnailFileExtension

	// Remove converted video folder
	err = os.RemoveAll(videoFolder)

	// TODO: Handle video data
	fmt.Printf("Video CID: %s\nThumbnail CID: %s\n", videoCID, thumbnailLocation)
	fmt.Printf("Video finished transcoding.\n")
}

// Simple file copy function
func fileCopy(src, dst string) error {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()
	_, err = io.Copy(destination, source)
	return nil
}

func encodingStatus(w http.ResponseWriter, r *http.Request) {
	keys, ok := r.URL.Query()["id"]

	if !ok || len(keys[0]) < 1 {
		fmt.Fprintf(w, "Url Param 'id' is missing")
		return
	}

	if progress, ok := encodingVideos.Videos[keys[0]]; ok {
		statusResponse := VideoEncodingStatusResponse{Progress: progress.CurrentProgress}

		json.NewEncoder(w).Encode(statusResponse)
	} else {
		fmt.Fprintf(w, "Specified ID is not transcoding.")
	}
}
