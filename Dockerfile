# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26.8-alpine AS build

WORKDIR /src
ARG TARGETOS
ARG TARGETARCH
COPY app/go.mod ./
RUN go mod download
COPY app/*.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/server .

FROM scratch

LABEL org.opencontainers.image.source="https://github.com/ssohbn/SFSU-csc648-06-Fall26-team04"
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/server /app/server
COPY --chown=65532:65532 app/static /app/static
USER 65532:65532
EXPOSE 6767
ENTRYPOINT ["/app/server"]
