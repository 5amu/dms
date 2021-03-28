#!/usr/bin/make -f

PKGNAME := dms
VERSION := 0.1

ARCH    ?= amd64
OS      ?= linux
DYNAMIC ?= 0

BUILD := ./bin

build:
	env CGO_ENABLED=$(DYNAMIC) GOOS=$(OS) GOARCH=$(ARCH) go build -o $(BUILD)/${PKGNAME}-${VERSION}-$(OS)-$(ARCH)

clean:
	[ -d $(BUILD) ] && rm -rf $(BUILD) || true