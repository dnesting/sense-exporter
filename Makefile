DOCKER_IMAGE ?= dnesting/sense-exporter
DOCKER_TAG ?= latest

.PHONY: docker-build
docker-build:
	docker buildx build \
		--push \
		--platform linux/arm/v7,linux/arm64/v8,linux/amd64 \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		.

docker-push:
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

test:
	go test -v ./...
