package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
	"github.com/pelletier/go-toml"
)

var daemonCmd = &cmds.Command{
	Run: func(r *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
		err := cli.HandleHelp("dapper", r, os.Stdout)
		if err == cli.ErrNoHelpRequested {
			readConfigFile()
			startDaemon()
			return nil
		}

		return nil
	},
}

var uploadCmd = &cmds.Command{
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

			defer resp.Body.Close()
			body, err = ioutil.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			if resp.StatusCode >= 400 {
				return re.Emit(fmt.Sprintf("Failed to send to dapper: %s\n", string(body)))
			}

			return re.Emit(fmt.Sprintf("%s", string(body)))
		}

		return nil
	},
}
