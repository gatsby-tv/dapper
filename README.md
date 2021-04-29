# dapper

Tool for adding and hosting videos on GatsbyTV.

Dapper uses IPFS to place content onto the network. It supports using an existing IPFS node to pin content, but also has the ability to run the IPFS node internally.

## Usage

In order to use dapper, first set the desired values in the configuration file. An example configuration file can be found [here](https://github.com/gatsby-tv/dapper/blob/main/configuration.toml.example). After it has been configured, place the configuration file in the same folder as the dapper executable, and run it with `dapper`. Dapper will then start listening for requests.

### Command Line Options

- `-p` - Port for dapper to listen for requests on.

## Building

To build dapper, simply clone this repository and run `go build` inside it. For example:

```bash
git clone https://github.com/gatsby-tv/dapper.git
cd dapper
go build
```

This will result in a `dapper` executable to be created in that folder (on Windows it will be `dapper.exe`).

## API

The dapper daemon listens for REST API requests on port 10000. This is used internally for uploading new videos, but can be communicated with directly.

### Routes

#### POST

`/video` - Add a video to Gatsby. No URL params.

Body:

```json
{
    "Title": "video title",
    "Description": "video description",
    "VideoFile": "path to video file on dapper's filesystem",
    "ThumbnailFile": "path to thumbnail file on dapper's filesystem",
}
```
