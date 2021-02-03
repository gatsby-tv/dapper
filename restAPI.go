package main

import (
	"bytes"
	"context"
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
}

type westeggNewVideoRequestBody struct {
	Title     string        `json:"title"`
	VidLen    int           `json:"duration"`
	VidHash   string        `json:"content"`
	Thumbnail thumbnailData `json:"thumbnail"`
	Channel   string        `json:"channel"`
}

type thumbnailData struct {
	ThumbHash string `json:"hash"`
	MimeType  string `json:"mimeType"`
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

	fmt.Fprintf(w, "Starting transcode of video file.\n")

	go func() {
		ctx := context.Background()
		defer ctx.Done()

		// Convert video to HLS pieces
		videoLength, err := getVideoLength(video.VideoFile)
		if err != nil {
			fmt.Printf("Unable to convert video to HLS: %s\n", err)
			return
		}
		videoFolder, err := convertToHLS(video.VideoFile)
		if err != nil {
			fmt.Printf("Unable to convert video to HLS: %s\n", err)
			return
		}

		// Process data into upload request for WestEgg

		videoCID, err := addFolderToIPFS(ctx, videoFolder)
		if err != nil {
			fmt.Printf("Unable to add video folder to IPFS: %s\n", err)
			return
		}

		thumbnailCID, err := addFileToIPFS(ctx, video.ThumbnailFile)
		if err != nil {
			fmt.Printf("Unable to add thumbnail to IPFS: %s\n", err)
			return
		}

		newVideo := westeggNewVideoRequestBody{
			Title:   video.Title,
			VidLen:  videoLength,
			VidHash: videoCID,
			Thumbnail: thumbnailData{
				ThumbHash: thumbnailCID,
				MimeType:  "image/jpeg",
			},
			Channel: video.Channel,
		}

		body, err := json.Marshal(newVideo)

		client := http.Client{}
		req, err := http.NewRequest(http.MethodPost, WesteggHost+"/v1/video", bytes.NewBuffer(body))

		if err != nil {
			fmt.Printf("Failed creating request for westegg: %s\n", err)
			return
		}

		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("Authorization", "Bearer "+authToken)

		resp, err := client.Do(req)

		if err != nil {
			fmt.Printf("Failed sending request to westegg: %s\n", err)
			return
		}

		defer resp.Body.Close()
		body, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("Failed reading body of response: %s\n", err)
			return
		}

		if resp.StatusCode >= 400 {
			fmt.Printf("Failed to send to westegg: %s\n", string(body))
			return
		}
		fmt.Printf("Response from westegg:\n%s\n", string(body))
	}()
}
