# yttv-bridge

Go daemon that exposes a local REST API for YouTube TV — channel guide,
Chromecast discovery, cast/stop

Backed by [`github.com/ygelfand/lib-yttv`](https://github.com/ygelfand/lib-yttv).

## Status

## Auth

paste `SAPISID` and `__Secure-3PSID` from a signed-in
Chromium DevTools session. Provide them as env vars:

```sh
docker run --rm --network host \
  -e YTTV_SAPISID=... \
  -e YTTV_SECURE_3PSID=... \
  ghcr.io/ygelfand/yttv-bridge:latest
```

`--network host` is required for mDNS Chromecast discovery.

## API

```
GET  /healthz
GET  /snapshot                    full state for HA to derive entities
GET  /channels
GET  /devices
POST /cast    {device, channel}
POST /stop    {device}
```
