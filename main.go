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
	"github.com/pelletier/go-toml"
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

// WesteggHost is the location to sent requests to westegg (eg. https://api.gatsby.sh)
var WesteggHost string

// DevMode changes the logic of some sections to corrospond with westegg's dev mode
var DevMode bool

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

				return nil
			},
		},
		"upload": &cmds.Command{
			Arguments: []cmds.Argument{
				cmds.StringArg("video data", false, false, "TOML file containing information about video to upload"),
			},
			Run: func(r *cmds.Request, re cmds.ResponseEmitter, e cmds.Environment) error {
				helpErr := cli.HandleHelp("dapper", r, os.Stdout)
				if helpErr == cli.ErrNoHelpRequested {
					if len(r.Arguments) == 0 {
						return re.Emit("Missing `video data` argument!\nDo `dapper upload --help` to see usage.")
					}
					if _, err := os.Stat(r.Arguments[0]); err != nil {
						return err
					}

					videoData, err := toml.LoadFile(r.Arguments[0])

					newVideo := newVideoRequestBody{
						Title:         videoData.Get("Title").(string),
						Description:   videoData.Get("Description").(string),
						VideoFile:     videoData.Get("VideoFile").(string),
						ThumbnailFile: videoData.Get("ThumbnailFile").(string),
						Channel:       videoData.Get("Channel").(string),
					}

					body, err := json.Marshal(newVideo)
					if err != nil {
						return err
					}

					client := http.Client{}
					req, err := http.NewRequest(http.MethodPost, "http://localhost:10000/video", bytes.NewBuffer(body))

					if err != nil {
						return err
					}

					req.Header.Add("Content-Type", "application/json")

					resp, err := client.Do(req)

					if err != nil {
						return err
					}

					if resp.StatusCode < 200 && resp.StatusCode >= 300 {
						defer resp.Body.Close()
						body, err := ioutil.ReadAll(resp.Body)
						if err != nil {
							return err
						}
						return re.Emit(fmt.Sprintf("Failed to send to dapper: %s", string(body)))
					}

					return nil
				}

				return nil
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
		log.Fatal(err)
	}

	req.Options["encoding"] = cmds.Text

	// create an emitter
	cliRe, err := cli.NewResponseEmitter(os.Stdout, os.Stderr, req)
	if err != nil {
		log.Fatal(err)
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

	DevMode = viper.GetBool("DevMode.devMode")
}

func getAuthToken() string {
	if DevMode {
		client := http.Client{}
		req, err := http.NewRequest(http.MethodGet, WesteggHost+"/v1/auth/devtoken?key=localhost&id="+viper.GetString("DevMode.userID"), nil)
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

	// TODO: Change to next-auth login
	data := loginRequestBody{
		Email:    viper.GetString("LoginInfo.userEmail"),
		Password: viper.GetString("LoginInfo.userPassword")}

	body, err := json.Marshal(data)

	client := http.Client{}
	req, err := http.NewRequest(http.MethodPost, WesteggHost+"/v1/auth/login", bytes.NewBuffer(body))

	if err != nil {
		log.Fatal(err)
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)

	if err != nil {
		log.Fatal(err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Failed to send to westegg: %s", string(body))
	}

	var res loginResponse

	json.NewDecoder(resp.Body).Decode(&res)

	return res.Token
}

func startDaemon() {
	WesteggHost = viper.GetString("DevMode.westeggHost")
	if WesteggHost == "" {
		WesteggHost = "https://api.gatsby.sh"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("-- Getting an IPFS node running -- ")

	err := startIPFS(ctx)

	if err != nil {
		log.Fatal(fmt.Errorf("Failed to start IPFS: %s", err))
	}

	authToken := getAuthToken()

	fmt.Println("\nReady for requests")
	handleRequests(authToken)
}
