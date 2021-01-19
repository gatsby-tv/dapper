package main

import (
	"fmt"
	"os/exec"
)

// HLSChunkLength - Size of HLS pieces in seconds
const HLSChunkLength = 10

func convertToHLS(videoFile string) string {
	videoFolder := "/home/nesbitt/test"
	exec.Command("ffmpeg -i \"" + videoFile + "\" -profile:v baseline -level 3.0 -s 1920x1080 -start_number 0 -hls_time " + fmt.Sprint(HLSChunkLength) + " -hls_list_size 0 -f hls \"" + videoFolder + "/master.m3u8\"")

	return videoFolder
}
