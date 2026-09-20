FROM golang:1.27.1-alpine@sha256:4cb7ac979db5fcc41cae44b2227ba5ab8a51e8807f40d9ba4dee20a0ad960b5b AS build
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
