# syntax=docker/dockerfile:1
ARG GO_VERSION=1.27.0
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 go test ./... && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
      -o /out/endlessnet-stun ./cmd/endlessnet-stun

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/endlessnet-stun /usr/local/bin/endlessnet-stun
USER 65532:65532
EXPOSE 3478/udp
ENTRYPOINT ["/usr/local/bin/endlessnet-stun"]
