package main

import (
	"os"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
)

// Emitters in this file should end with a newline since they will be printed on the command line.

var daemonCmd = &cmds.Command{
	Options: []cmds.Option{
		cmds.IntOption("p", "port", "Port for the daemon to listen on."),
	},
	Run: func(r *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) error {
		err := cli.HandleHelp("dapper", r, os.Stdout)
		if err == cli.ErrNoHelpRequested {
			readConfigFile()
			var daemonPort int
			if portOption := r.Options["p"]; portOption == nil {
				daemonPort = 10000
			} else {
				daemonPort = r.Options["p"].(int)
			}
			startDaemon(daemonPort)
			return nil
		} else if err == nil {
			return nil
		} else {
			return err
		}
	},
}
