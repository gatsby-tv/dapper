package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
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

// Location to send westegg requests
var westeggHost string

// devMode changes the logic of some sections to corrospond with westegg's dev mode
var devMode bool

var rootCmd = &cmds.Command{
	Options: []cmds.Option{
		cmds.BoolOption(cmds.OptLongHelp, "Show the full command help text."),
		cmds.BoolOption(cmds.OptShortHelp, "Show a short version of the command help text."),
	},
	Subcommands: map[string]*cmds.Command{
		"daemon": daemonCmd,
		"upload": uploadCmd,
	},
	Run: func(r *cmds.Request, re cmds.ResponseEmitter, e cmds.Environment) error {
		cli.HandleHelp("dapper", r, os.Stdout)
		return nil
	},
}

func main() {
	// Handle `dapper version` or `dapper help`
	if len(os.Args) > 1 {
		// Handle `dapper --version`
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

	// Parse the command path, arguments and options from the command line
	req, err := cli.Parse(context.TODO(), os.Args[1:], os.Stdin, rootCmd)
	if err != nil {
		log.Fatal(err)
	}

	req.Options["encoding"] = cmds.Text

	// Create an emitter
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
	viper.SetConfigName(configFileName)
	viper.SetConfigType(configFileExtension)
	viper.AddConfigPath(configFileLocation)
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatal(err)
	}

	// Verify necessary config values are set
	if videoDir := viper.GetString("Videos.videoStorageFolder"); videoDir == "" {
		videoDir, err = homedir.Dir()
		if err != nil {
			log.Fatal(err)
		}
		viper.Set("Videos.videoStorageFolder", path.Join(videoDir, "Videos"))
	}

	if ffmpegDir := viper.GetString("ffmpeg.ffmpegDir"); ffmpegDir == "" {
		viper.Set("ffmpeg.ffmpegDir", "ffmpeg")
	}

	if ffmpegDir := viper.GetString("ffmpeg.ffprobeDir"); ffmpegDir == "" {
		viper.Set("ffmpeg.ffprobeDir", "ffprobe")
	}
}

func startDaemon(port int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("-- Setting up IPFS --")

	err := startIPFS(ctx)
	if err != nil {
		log.Fatal(fmt.Errorf("Failed to start IPFS: %s", err))
	}

	fmt.Println("Ready for requests")
	handleRequests(port)
}
