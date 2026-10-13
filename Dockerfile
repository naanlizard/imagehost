FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
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
