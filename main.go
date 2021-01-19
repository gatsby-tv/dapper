package main

import (
	"context"
	"fmt"
	// This package is needed so that all the preloaded plugins are loaded automatically
)

// f"ffmpeg -i \"{video}\" -profile:v baseline -level 3.0 -s 1920x1080 -start_number 0 -hls_time {HLS_CHUNK_LENGTH} -hls_list_size 0 -f hls \"{video_folder}/master.m3u8\""

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("-- Getting an IPFS node running -- ")

	err := startIPFS(ctx)

	if err != nil {
		panic(fmt.Errorf("Failed to start IPFS: %s", err))
	}
}
