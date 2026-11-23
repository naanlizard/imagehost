FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
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
