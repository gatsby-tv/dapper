package main

import (
	"context"
	"flag"
	"os"
	"path"

	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

// Folder to search for config file
const configFileLocation = "."

// Name of config file (without extension)
const configFileName = "configuration"

// Type of config file
const configFileExtension = "toml"

func main() {
	log.Info().Msgf("Dapper version: %s-%s", CurrentVersionNumber, CurrentCommit)
	readConfigFile()
	log.Trace().Msg("Successfully loaded config")

	portPtr := flag.Int("p", 10000, "Port to listen for requests on.")
	flag.Parse()

	// Verify the given port is a valid port number
	if *portPtr < 1 || *portPtr > 65535 {
		log.Fatal().Msg("Invalid port specified.")
	}

	// Create video scratch path if it does not exist
	if _, err := os.Stat(path.Join(viper.GetString("Videos.TempVideoStorageFolder"), videoScratchFolder)); os.IsNotExist(err) {
		err := os.Mkdir(path.Join(viper.GetString("Videos.TempVideoStorageFolder"), videoScratchFolder), 0755)
		if err != nil {
			log.Fatal().Msgf("Failed setting up video directory: %s", err)
		}
	}

	// Setup memory map for keeping track of videos being processed
	encodingVideos.Videos = make(map[string]EncodingVideo)

	startDaemon(*portPtr)
}

// Read in config values to viper and check that necessary values are set
func readConfigFile() {
	viper.SetConfigName(configFileName)
	viper.SetConfigType(configFileExtension)
	viper.AddConfigPath(configFileLocation)
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found ignore error and use defaults
			log.Info().Msg("Configuration file not found, using defaults.")
		} else {
			log.Fatal().Msg(err.Error())
		}
	}

	// Verify necessary config values are set
	if videoDir := viper.GetString("Videos.TempVideoStorageFolder"); videoDir == "" {
		videoDir, err := os.MkdirTemp(os.TempDir(), "dapper-*")
		if err != nil {
			log.Fatal().Msg(err.Error())
		}
		viper.Set("Videos.TempVideoStorageFolder", videoDir)
	}

	// If none was set, use the one on the path
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

	log.Trace().Msg("Setting up IPFS")

	err := startIPFS(ctx)
	if err != nil {
		log.Fatal().Msgf("Failed to start IPFS: %s", err)
	}

	log.Info().Msg("Ready for requests")
	handleRequests(port)
}
