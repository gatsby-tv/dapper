package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
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

var rootCmd = &cmds.Command{
	Options: []cmds.Option{
		cmds.BoolOption(cmds.OptLongHelp, "Show the full command help text."),
		cmds.BoolOption(cmds.OptShortHelp, "Show a short version of the command help text."),
	},
	Subcommands: map[string]*cmds.Command{
		"daemon": &cmds.Command{
			Run: func(r *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
				err := cli.HandleHelp("dapper", r, os.Stdout)
				if err == cli.ErrNoHelpRequested {
					readConfigFile()
					startDaemon()
					return nil
				}

				return err
			},
		},
		"upload": &cmds.Command{
			// Arguments: []cmds.Argument{
			// 	cmds.StringArg("video", false, false, "Video file to upload"),
			// 	cmds.StringArg("description", false, false, "Description of the video"),
			// 	cmds.StringArg("title", false, false, "Title of the video"),
			// 	cmds.StringArg("thumbnail", false, false, "Thumbnail for the video"),
			// 	cmds.StringArg("channel", false, false, "Channel to upload video to"),
			// 	cmds.StringArg("Uploadable", false, false, "Ignore"),
			// },
			Run: func(r *cmds.Request, re cmds.ResponseEmitter, e cmds.Environment) error {
				err := cli.HandleHelp("dapper", r, os.Stdout)
				if err == cli.ErrNoHelpRequested {
					//TODO: Get data from user
					newVideo := newVideoRequestBody{
						Title:         r.Arguments[0],
						Description:   r.Arguments[0],
						VideoFile:     r.Arguments[0],
						ThumbnailFile: r.Arguments[0],
						Channel:       r.Arguments[0],
						Show:          r.Arguments[0],
					}

					body, err := json.Marshal(newVideo)
					if err != nil {
						return err
					}

					return re.Emit(fmt.Sprintf("Failed to send to dapper: %s", string(body)))

					// client := http.Client{}
					// req, err := http.NewRequest(http.MethodPost, "localhost:10000/video", bytes.NewBuffer(body))

					// if err != nil {
					// 	return err
					// }

					// req.Header.Add("Content-Type", "application/json")
					// req.Header.Add("Authorization", "Bearer "+authToken)

					// resp, err := client.Do(req)

					// if err != nil {
					// 	return err
					// }

					// if resp.StatusCode < 200 && resp.StatusCode >= 300 {
					// 	defer resp.Body.Close()
					// 	body, err := ioutil.ReadAll(resp.Body)
					// 	if err != nil {
					// 		return err
					// 	}
					// 	return re.Emit(fmt.Sprintf("Failed to send to dapper: %s", string(body)))
					// }
				}

				return err
			},
		},
	},
	Run: func(r *cmds.Request, re cmds.ResponseEmitter, e cmds.Environment) error {
		cli.HandleHelp("dapper", r, os.Stdout)
		return nil
	},
}

func main() {
	// Handle `dapper version` or `dapper help`
	if len(os.Args) > 1 {
		// Handle `dapper --version'
		if os.Args[1] == "--version" {
			os.Args[1] = "version"
		}

		//Handle `dapper help` and `dapper help <sub-command>`
		if os.Args[1] == "help" {
			if len(os.Args) > 2 {
				os.Args = append(os.Args[:1], os.Args[2:]...)
				// Handle `dapper help --help`
				// append `--help`,when the command is not `dapper help --help`
				if os.Args[1] != "--help" {
					os.Args = append(os.Args, "--help")
				}
			} else {
				os.Args[1] = "--help"
			}
		}
	} else if len(os.Args) == 1 {
		os.Args = append(os.Args, "--help")
	}

	// parse the command path, arguments and options from the command line
	req, err := cli.Parse(context.TODO(), os.Args[1:], os.Stdin, rootCmd)
	if err != nil {
		panic(err)
	}

	req.Options["encoding"] = cmds.Text

	// create an emitter
	cliRe, err := cli.NewResponseEmitter(os.Stdout, os.Stderr, req)
	if err != nil {
		panic(err)
	}

	wait := make(chan struct{})
	var re cmds.ResponseEmitter = cliRe
	if pr, ok := req.Command.PostRun[cmds.CLI]; ok {
		var (
			res   cmds.Response
			lower = re
		)

		re, res = cmds.NewChanResponsePair(req)

		go func() {
			defer close(wait)
			err := pr(res, lower)
			if err != nil {
				fmt.Println("error: ", err)
			}
		}()
	} else {
		close(wait)
	}

	rootCmd.Call(req, re, nil)
	<-wait

	os.Exit(cliRe.Status())
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

	if resp.StatusCode != 200 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		fmt.Printf("Failed to send to westegg: %s", string(body))
	}

	var res loginResponse

	json.NewDecoder(resp.Body).Decode(&res)

	return res.Token
}

func startDaemon() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("-- Getting an IPFS node running -- ")

	err := startIPFS(ctx)

	if err != nil {
		panic(fmt.Errorf("Failed to start IPFS: %s", err))
	}

	authToken := getAuthToken()

	fmt.Println("\nReady for requests")
	handleRequests(authToken)
}
