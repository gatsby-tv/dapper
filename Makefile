APP_NAME = dapper
APP_VERSION ?= $(shell grep 'const CurrentVersionNumber =' version.go | cut -d '"' -f2)
BUILD ?= $(shell git rev-parse --short HEAD)

GOFLAGS =-ldflags="-X main.CurrentCommit=$(BUILD)"

build:
	docker build -t gatsbytv/$(APP_NAME):$(APP_VERSION)-$(BUILD) -t gatsbytv/$(APP_NAME):latest .

push:
	docker push gatsbytv/$(APP_NAME):$(APP_VERSION)-$(BUILD) --all-tags
