FROM golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
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
