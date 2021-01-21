package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/spf13/viper"
)

// ConfigFileLocation - Folder to search for config file
const ConfigFileLocation = "."

// ConfigFileName - Name of config file (without extension)
const ConfigFileName = "configuration"

// ConfigFileExtension - Type of config file
const ConfigFileExtension = "toml"

func main() {
	readConfigFile()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("-- Getting an IPFS node running -- ")

	err := startIPFS(ctx)

	if err != nil {
		panic(fmt.Errorf("Failed to start IPFS: %s", err))
	}

	convertToHLS(os.Args[1])

	fmt.Println("\nReady for requests")
	testRest()
}

func readConfigFile() {
	viper.SetConfigName(ConfigFileName)
	viper.SetConfigType(ConfigFileExtension)
	viper.AddConfigPath(ConfigFileLocation)
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatal(err)
	}
}
