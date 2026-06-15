package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ygelfand/lib-yttv/cast"
	"github.com/ygelfand/lib-yttv/epg"
	"github.com/ygelfand/yttv-bridge/internal/state"
)

type handlers struct {
	store *state.Store
}

type airingJSON struct {
	VideoID      string `json:"video_id"`
	Title        string `json:"title"`
	Subtitle     string `json:"subtitle"`
	Synopsis     string `json:"synopsis"`
	ThumbnailURL string `json:"thumbnail_url"`
	StartsAt     string `json:"starts_at"`
	EndsAt       string `json:"ends_at"`
	IsLive       bool   `json:"is_live"`
}

type channelJSON struct {
	Name          string       `json:"name"`
	IconURL       string       `json:"icon_url"`
	CurrentAiring *airingJSON  `json:"current_airing,omitempty"`
	NextAirings   []airingJSON `json:"next_airings"`
}

type deviceJSON struct {
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

type castReq struct {
	Device  string `json:"device"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Channel string `json:"channel"`
}

type stopReq struct {
	Device string `json:"device"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

func (h *handlers) channels(c *gin.Context) {
	c.JSON(http.StatusOK, mapChannels(h.store.Channels()))
}

func (h *handlers) devices(c *gin.Context) {
	c.JSON(http.StatusOK, mapDevices(h.store.Devices()))
}

func (h *handlers) snapshot(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"channels": mapChannels(h.store.Channels()),
		"devices":  mapDevices(h.store.Devices()),
	})
}

func (h *handlers) cast(c *gin.Context) {
	var req castReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if req.Channel == "" {
		fail(c, http.StatusBadRequest, errors.New("channel required"))
		return
	}
	dev, err := h.resolveDevice(req.Device, req.Host, req.Port)
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	if err := h.store.Session.Cast(ctx, dev, req.Channel); err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handlers) stop(c *gin.Context) {
	var req stopReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	dev, err := h.resolveDevice(req.Device, req.Host, req.Port)
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	recv, err := cast.Connect(ctx, dev)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	defer recv.Close()
	if err := recv.Stop(ctx); err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handlers) resolveDevice(name, host string, port int) (cast.Device, error) {
	if host != "" {
		if port == 0 {
			port = 8009
		}
		return cast.Device{Name: host, Host: host, Port: port}, nil
	}
	if name == "" {
		return cast.Device{}, errors.New("device or host required")
	}
	dev, ok := h.store.FindDevice(name)
	if !ok {
		return cast.Device{}, errors.New("device not found: " + name)
	}
	return dev, nil
}

func mapChannels(in []epg.Channel) []channelJSON {
	out := make([]channelJSON, 0, len(in))
	now := time.Now().UnixMilli()
	horizon := now + (2 * time.Hour).Milliseconds()
	for _, ch := range in {
		row := channelJSON{
			Name:        ch.Name,
			IconURL:     normalizeURL(ch.StationIconURL),
			NextAirings: []airingJSON{},
		}
		for _, a := range ch.Airings {
			if a.EndTimeMs <= now {
				continue
			}
			aj := airingJSON{
				VideoID:      a.VideoID,
				Title:        a.Title,
				Subtitle:     a.Subtitle,
				Synopsis:     a.Synopsis,
				ThumbnailURL: normalizeURL(a.ThumbnailURL),
				StartsAt:     time.UnixMilli(a.BeginTimeMs).UTC().Format(time.RFC3339),
				EndsAt:       time.UnixMilli(a.EndTimeMs).UTC().Format(time.RFC3339),
				IsLive:       a.IsLive,
			}
			if a.IsLive && row.CurrentAiring == nil {
				cur := aj
				row.CurrentAiring = &cur
				continue
			}
			if a.BeginTimeMs >= now && a.BeginTimeMs < horizon {
				row.NextAirings = append(row.NextAirings, aj)
			}
		}
		out = append(out, row)
	}
	return out
}

func mapDevices(in []cast.Device) []deviceJSON {
	out := make([]deviceJSON, 0, len(in))
	for _, d := range in {
		out = append(out, deviceJSON{Name: d.Name, Host: d.Host, Port: d.Port})
	}
	return out
}

func normalizeURL(s string) string {
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	return s
}

func fail(c *gin.Context, status int, err error) {
	c.JSON(status, gin.H{"error": err.Error()})
}
