package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/dustin/go-humanize"
	"github.com/gorilla/mux"
)

type newVideoRequestBody struct {
	Title         string `json:"Title"`
	Description   string `json:"Description"`
	VideoFile     string `json:"VideoFile"`
	ThumbnailFile string `json:"ThumbnailFile"`
	Channel       string `json:"Channel"`
	Show          string `json:"Show"`
}

type westeggNewVideoRequestBody struct {
	Token      string `json:"token"`
	Title      string `json:"title"`
	Desc       string `json:"description"`
	VidHash    string `json:"hash"`
	ThumbHash  string `json:"thumbnailHash"`
	Channel    string `json:"id"`
	Uploadable string `json:"uploadable"`
}

var authToken string

func handleRequests(token string) {
	authToken = token

	myRouter := mux.NewRouter().StrictSlash(true)

	// GETs
	myRouter.HandleFunc("/", homePage)
	myRouter.HandleFunc("/traffic", getCurrentOutTraffic).Methods("GET")

	// POSTs
	myRouter.HandleFunc("/video", uploadVideo).Methods("POST")

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
	var video newVideoRequestBody
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

	newVideo := westeggNewVideoRequestBody{
		Token:      authToken,
		Title:      video.Title,
		Desc:       video.Description,
		VidHash:    videoCID,
		ThumbHash:  thumbnailCID,
		Channel:    video.Channel,
		Uploadable: video.Show,
	}

	body, err := json.Marshal(newVideo)

	client := http.Client{}
	req, err := http.NewRequest(http.MethodPost, "https://api.gatsby.sh/video", bytes.NewBuffer(body))

	if err != nil {
		panic(err)
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+authToken)

	resp, err := client.Do(req)

	if err != nil {
		panic(err)
	}

	if resp.StatusCode < 200 && resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(w, "Failed to send to westegg: %s", string(body))
	}
}

func testRest(authToken string) {
	handleRequests(authToken)
}
