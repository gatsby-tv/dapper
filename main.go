package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

type loginRequestBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

// Folder to search for config file
const configFileLocation = "."

// Name of config file (without extension)
const configFileName = "configuration"

// Type of config file
const configFileExtension = "toml"

func main() {
	readConfigFile()
	var daemonPort int
	// TODO: Change to traditional command line args
	// if portOption := r.Options["p"]; portOption == nil {
	daemonPort = 10000
	// } else {
	// 	daemonPort = r.Options["p"].(int)
	// }

	if _, err := os.Stat(path.Join(viper.GetString("Videos.TempVideoStorageFolder"), videoScratchFolder)); os.IsNotExist(err) {
		err := os.Mkdir(path.Join(viper.GetString("Videos.TempVideoStorageFolder"), videoScratchFolder), 0755)
		if err != nil {
			log.Fatalf("Failed setting up video directory: %s", err)
		}
	}

	startDaemon(daemonPort)
}

// Read in config values to viper and check that necessary values are set
func readConfigFile() {
	viper.SetConfigName(configFileName)
	viper.SetConfigType(configFileExtension)
	viper.AddConfigPath(configFileLocation)
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatal(err)
	}

	// Verify necessary config values are set
	if videoDir := viper.GetString("Videos.TempVideoStorageFolder"); videoDir == "" {
		videoDir, err = homedir.Dir()
		if err != nil {
			log.Fatal(err)
		}
		viper.Set("Videos.TempVideoStorageFolder", path.Join(videoDir, "Videos"))
	}

	if ffmpegDir := viper.GetString("ffmpeg.ffmpegDir"); ffmpegDir == "" {
		viper.Set("ffmpeg.ffmpegDir", "ffmpeg")
	}

	if ffmpegDir := viper.GetString("ffmpeg.ffprobeDir"); ffmpegDir == "" {
		viper.Set("ffmpeg.ffprobeDir", "ffprobe")
	}
}

// Setup IPFS and start listening for requests
func startDaemon(port int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("-- Setting up IPFS --")

	err := startIPFS(ctx)
	if err != nil {
		log.Fatalf("Failed to start IPFS: %s", err)
	}

	fmt.Println("Ready for requests")
	handleRequests(port)
}
