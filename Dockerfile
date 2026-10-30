FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
WORKDIR /src
COPY go.mod *.go ./
COPY templates ./templates
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /imagehost . && mkdir /data

FROM scratch
LABEL org.opencontainers.image.source=https://github.com/naanlizard/imagehost
COPY --from=build /imagehost /imagehost
COPY --from=build --chown=1000:1000 /data /data
USER 1000:1000
ENTRYPOINT ["/imagehost"]
