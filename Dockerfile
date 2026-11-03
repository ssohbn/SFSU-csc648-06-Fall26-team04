# go-sqlite3 needs cgo, so build on debian with gcc and run on distroless,
# which has glibc but no shell and runs as the nonroot user
FROM golang:1.26-trixie AS build
WORKDIR /src
COPY app/go.mod app/go.sum ./
RUN go mod download
COPY app/ ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/site . && mkdir /out/data

FROM gcr.io/distroless/base-debian13:nonroot
COPY --from=build /out/site /app/site
# a new docker volume copies this owner, so the app can write its database there
COPY --from=build --chown=65532:65532 /out/data /data
ENV DB_PATH=/data/studysync.sqlite3
ENTRYPOINT ["/app/site"]
