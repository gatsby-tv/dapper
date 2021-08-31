FROM golang:1.16.3 AS build
WORKDIR /dapper
ARG BUILD
COPY *.go go.mod go.sum ./
RUN go build -ldflags "-X main.CurrentCommit=$BUILD" -o dapper

FROM linuxserver/ffmpeg:version-4.3-cli
WORKDIR /dapper
COPY --from=build dapper ./
COPY configuration.toml.docker ./configuration.toml

VOLUME /scratch
VOLUME /ipfs

EXPOSE 10000

ENTRYPOINT ["/dapper/dapper"]
