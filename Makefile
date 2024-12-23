DOCKER_IMAGE ?= dnesting/sense-exporter
DOCKER_TAG ?= latest

DATE=$(shell TZ=UTC date +"%Y-%m-%dT%H:%M:%SZ")

TAG_COMMIT := $(shell git rev-list --abbrev-commit --tags --max-count=1)
TAG := $(shell git describe --abbrev=0 --tags ${TAG_COMMIT} 2>/dev/null || true)
COMMIT := $(shell git rev-parse --short HEAD)
DATE := $(shell git log -1 --format=%cd --date=format:"%Y%m%d")
VERSION := $(TAG:v%=%)
ifneq ($(COMMIT), $(TAG_COMMIT))
	VERSION := $(VERSION)-next-$(COMMIT)-$(DATE)
endif
ifeq ($(VERSION),)
	VERSION := $(COMMIT)-$(DATE)
endif
ifneq ($(shell git status --porcelain),)
	VERSION := $(VERSION)-dirty
endif

all: write-version docker-build docker-push

write-version:
	@echo "package main" >cmd/sense-exporter/version.go
	@echo "const (" >>cmd/sense-exporter/version.go
	@echo "  Version = \"$(VERSION)\"" >>cmd/sense-exporter/version.go
	@echo "  BuildDate = \"$(DATE)\"" >>cmd/sense-exporter/version.go
	@echo ")" >>cmd/sense-exporter/version.go
	@echo "// cmd/sense-exporter/version.go"
	@cat cmd/sense-exporter/version.go

.PHONY: docker-build
docker-build:
	docker buildx build \
		--push \
		--platform linux/arm/v7,linux/arm64/v8,linux/amd64 \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		.

#docker-push:
#	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

test:
	go test -v ./...
