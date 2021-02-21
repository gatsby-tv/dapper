package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
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

type westeggChannelResponse struct {
	ID string `json:"_id"`
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
	var videoToUpload newVideoRequestBody
	err := json.Unmarshal(reqBody, &videoToUpload)
	if err != nil {
		fmt.Fprintf(w, "Unable to unmarshal json body: %s", err)
		return
	}

	// Get ID associated with given channel handle
	client := http.Client{}
	req, err := http.NewRequest(http.MethodGet, westeggHost+"/v1/channel/"+videoToUpload.Channel, nil)
	if err != nil {
		fmt.Fprintf(w, "Failed creating request for westegg: %s", err)
		return
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(w, "Failed getting channel ID from westegg: %s", err)
		return
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(w, "Failed reading body of response: %s", err)
		return
	}

	if resp.StatusCode >= 400 {
		fmt.Fprintf(w, "Failed getting channel ID from westegg: %s", string(body))
		return
	}

	var channelID westeggChannelResponse
	err = json.Unmarshal(body, &channelID)

	videoToUpload.Channel = channelID.ID

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

	newVideo := westeggNewVideoRequestBody{
		Title:   videoToUpload.Title,
		VidLen:  videoLength,
		VidHash: videoCID,
		Thumbnail: thumbnailData{
			ThumbHash: thumbnailLocation,
			MimeType:  mime.TypeByExtension(thumbnailFileExtension),
		},
		Channel: videoToUpload.Channel,
	}

	body, err := json.Marshal(newVideo)

	client := http.Client{}
	req, err := http.NewRequest(http.MethodPost, westeggHost+"/v1/video", bytes.NewBuffer(body))
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
		fmt.Printf("Error response sending video to westegg: %s\n", string(body))
		return
	}
	fmt.Printf("Response from westegg:\n%s\n", string(body))
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
