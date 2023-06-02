DOCKER_IMAGE ?= dnesting/sense-exporter
DOCKER_TAG ?= latest

DATE=$(shell TZ=UTC date +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(shell git describe --tags --abbrev=8 --dirty --always --long)

.PHONY: docker-build
docker-build:
	docker buildx build \
		--build-arg DATE="$(DATE)" \
		--build-arg GIT_COMMIT="$(GIT_COMMIT)" \
		--push \
		--platform linux/arm/v7,linux/arm64/v8,linux/amd64 \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		.

docker-push:
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

test:
	go test -v ./...
