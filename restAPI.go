package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/dustin/go-humanize"
	"github.com/gorilla/mux"
)

type newVideoRequestBody struct {
	Title         string `json:"Title"`
	Description   string `json:"Description"`
	VideoFile     string `json:"VideoFile"`
	ThumbnailFile string `json:"ThumbnailFile"`
	Channel       string `json:"Channel"`
}

type thumbnailData struct {
	ThumbHash string `json:"hash"`
	MimeType  string `json:"mimeType"`
}

func handleRequests(port int) {
	myRouter := mux.NewRouter().StrictSlash(true)

	// GETs
	myRouter.HandleFunc("/", homePage)
	myRouter.HandleFunc("/traffic", getCurrentOutTraffic).Methods("GET")

	// POSTs
	myRouter.HandleFunc("/video", uploadVideo).Methods("POST")

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), myRouter))
}

func homePage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Welcome to the Home Page!")
}

func getCurrentOutTraffic(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "%s/s", humanize.Bytes(uint64(Reporter.GetBandwidthTotals().RateOut)))
}

func uploadVideo(w http.ResponseWriter, r *http.Request) {
	reqBody, _ := ioutil.ReadAll(r.Body)
	var videoToUpload newVideoRequestBody
	err := json.Unmarshal(reqBody, &videoToUpload)
	if err != nil {
		fmt.Fprintf(w, "Unable to unmarshal json body: %s", err)
		return
	}

	fmt.Fprintf(w, "Starting transcode of video file.")

	// Run rest of video upload async
	go asyncVideoUpload(videoToUpload)
}

func asyncVideoUpload(videoToUpload newVideoRequestBody) {
	ctx := context.Background()
	defer ctx.Done()

	// Convert video to HLS pieces
	videoLength, err := getVideoLength(videoToUpload.VideoFile)
	if err != nil {
		fmt.Printf("Unable to get video length: %s\n", err)
		return
	}

	fmt.Printf("Video Length: %d", videoLength)

	videoFolder, err := convertToHLS(videoToUpload.VideoFile)
	if err != nil {
		fmt.Printf("Unable to convert video to HLS: %s\n", err)
		return
	}

	thumbnailFileExtension := filepath.Ext(videoToUpload.ThumbnailFile)
	if err = fileCopy(videoToUpload.ThumbnailFile, path.Join(videoFolder, "thumbnail"+thumbnailFileExtension)); err != nil {
		fmt.Printf("Unable to copy thumbnail file: %s\n", err)
		return
	}

	// Process data into upload request for WestEgg

	videoCID, err := addFolderToIPFS(ctx, videoFolder)
	if err != nil {
		fmt.Printf("Unable to add video folder to IPFS: %s\n", err)
		return
	}

	thumbnailLocation := videoCID + "/thumbnail" + thumbnailFileExtension

	// newVideo := westeggNewVideoRequestBody{
	// 	Title:   videoToUpload.Title,
	// 	VidLen:  videoLength,
	// 	VidHash: videoCID,
	// 	Thumbnail: thumbnailData{
	// 		ThumbHash: thumbnailLocation,
	// 		MimeType:  mime.TypeByExtension(thumbnailFileExtension),
	// 	},
	// 	Channel: videoToUpload.Channel,
	// }

	fmt.Printf("Video CID: %s\nThumbnail CID: %s\n", videoCID, thumbnailLocation)
	fmt.Printf("Video finished transcoding.\n")
}

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
