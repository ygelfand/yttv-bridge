FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/yttv-bridge .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tini && adduser -D -u 10001 yttv
COPY --from=build /out/yttv-bridge /usr/local/bin/yttv-bridge
USER yttv
ENV YTTV_LISTEN=:8765 \
    YTTV_LOG_FORMAT=json
EXPOSE 8765
ENTRYPOINT ["/sbin/tini","--","/usr/local/bin/yttv-bridge","serve"]
