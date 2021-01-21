package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/dustin/go-humanize"
	"github.com/gorilla/mux"
)

type videoData struct {
	Title         string `json:"Title"`
	Description   string `json:"Description"`
	Tags          string `json:"Tags"`
	VideoFile     string `json:"VideoFile"`
	ThumbnailFile string `json:"ThumbnailFile"`
	Channel       string `json:"Channel"`
	Show          string `json:"Show"`
}

func handleRequests() {
	myRouter := mux.NewRouter().StrictSlash(true)

	// GETs
	myRouter.HandleFunc("/", homePage)
	myRouter.HandleFunc("/traffic", getCurrentOutTraffic)

	// POSTs
	myRouter.HandleFunc("/video/new", uploadVideo).Methods("POST")

	log.Fatal(http.ListenAndServe(":10000", myRouter))
}

func homePage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Welcome to the Home Page!")
}

func getCurrentOutTraffic(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "%s/s", humanize.Bytes(uint64(Reporter.GetBandwidthTotals().RateOut)))
}

func uploadVideo(w http.ResponseWriter, r *http.Request) {
	reqBody, _ := ioutil.ReadAll(r.Body)
	var video videoData
	err := json.Unmarshal(reqBody, &video)
	if err != nil {
		fmt.Fprintf(w, "Unable to unmarshal json body: %s", err)
		return
	}

	// Validate request data
	// 		Validate User Credentials
	// 		Validate Channel Existence and writeability
	// 		Validate Show Existence and writeability

	// Save video and thumbnail files to disk

	// Convert video to HLS pieces
	videoFolder, err := convertToHLS(video.VideoFile)
	if err != nil {
		fmt.Fprintf(w, "Unable to convert video to HLS: %s", err)
		return
	}

	// Process data into upload request for WestEgg

	ctx := r.Context()

	videoCID, err := addFolderToIPFS(ctx, videoFolder)
	if err != nil {
		fmt.Fprintf(w, "Unable to add video folder to IPFS: %s", err)
		return
	}

	thumbnailCID, err := addFileToIPFS(ctx, video.ThumbnailFile)
	if err != nil {
		fmt.Fprintf(w, "Unable to add thumbnail to IPFS: %s", err)
		return
	}

	var newVideo videoData = videoData{
		Title:         video.Title,
		Description:   video.Description,
		Tags:          video.Tags,
		VideoFile:     videoCID,
		ThumbnailFile: thumbnailCID,
		Channel:       video.Channel,
		Show:          video.Show,
	}

	json.NewEncoder(w).Encode(newVideo)
}

func testRest() {
	handleRequests()
}
