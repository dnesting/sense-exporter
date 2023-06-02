FROM --platform=$BUILDPLATFORM golang:bullseye AS build

WORKDIR /src
COPY . .

ARG TARGETOS TARGETARCH
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /bin/sense-exporter ./cmd/sense-exporter

# -----

FROM gcr.io/distroless/static-debian11:nonroot

COPY --from=build /bin/sense-exporter /sense-exporter
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENTRYPOINT ["/sense-exporter"]
USER nonroot
EXPOSE 9553
