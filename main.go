package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/spf13/viper"
)

type loginRequestBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

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

	authToken := getAuthToken()

	fmt.Println("\nReady for requests")
	testRest(authToken)
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

func getAuthToken() string {
	data := loginRequestBody{
		Email:    viper.GetString("userEmail"),
		Password: viper.GetString("userPassword")}

	body, err := json.Marshal(data)

	client := http.Client{}
	req, err := http.NewRequest(http.MethodPost, "https://api.gatsby.sh/auth/login", bytes.NewBuffer(body))

	if err != nil {
		panic(err)
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)

	if err != nil {
		panic(err)
	}

	defer resp.Body.Close()

	var res loginResponse

	json.NewDecoder(resp.Body).Decode(&res)

	return res.Token
}
