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
	Name             string       `json:"name"`
	IconURL          string       `json:"icon_url"`
	LiveThumbnailURL string       `json:"live_thumbnail_url,omitempty"`
	CurrentAiring    *airingJSON  `json:"current_airing,omitempty"`
	NextAirings      []airingJSON `json:"next_airings"`
}

type deviceJSON struct {
	Name   string            `json:"name"`
	Host   string            `json:"host"`
	Port   int               `json:"port"`
	UUID   string            `json:"uuid,omitempty"`
	Video  bool              `json:"video"`
	Status *deviceStatusJSON `json:"status,omitempty"`
}

type deviceStatusJSON struct {
	App         string  `json:"app,omitempty"`
	Idle        bool    `json:"idle"`
	Volume      float64 `json:"volume"`
	Muted       bool    `json:"muted"`
	PlayerState string  `json:"player_state,omitempty"`
	Title       string  `json:"title,omitempty"`
	Subtitle    string  `json:"subtitle,omitempty"`
	ContentID   string  `json:"content_id,omitempty"`
	Channel     string  `json:"channel,omitempty"` // matched from EPG via content_id
	StreamType  string  `json:"stream_type,omitempty"`
	Live        bool    `json:"live"`
	ImageURL    string  `json:"image_url,omitempty"`
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
	c.JSON(http.StatusOK, mapDevices(h.store.DeviceStatuses(), h.channelByVideoID()))
}

func (h *handlers) snapshot(c *gin.Context) {
	byVID := h.channelByVideoID()
	c.JSON(http.StatusOK, gin.H{
		"channels": mapChannels(h.store.Channels()),
		"devices":  mapDevices(h.store.DeviceStatuses(), byVID),
	})
}

// channelByVideoID indexes channel names by their live videoId so a device's
// playing content_id can be resolved to a channel.
func (h *handlers) channelByVideoID() map[string]string {
	chs := h.store.Channels()
	m := make(map[string]string, len(chs))
	for _, ch := range chs {
		if ch.LiveVideoID != "" {
			m[ch.LiveVideoID] = ch.Name
		}
	}
	return m
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

type volumeReq struct {
	Device string  `json:"device"`
	Host   string  `json:"host"`
	Port   int     `json:"port"`
	Level  float64 `json:"level"`
}

func (h *handlers) volume(c *gin.Context) {
	var req volumeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	h.withReceiver(c, req.Device, req.Host, req.Port, func(_ context.Context, r *cast.Receiver) error {
		return r.SetVolume(req.Level)
	})
}

type muteReq struct {
	Device string `json:"device"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Muted  bool   `json:"muted"`
}

func (h *handlers) mute(c *gin.Context) {
	var req muteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	h.withReceiver(c, req.Device, req.Host, req.Port, func(_ context.Context, r *cast.Receiver) error {
		return r.SetMuted(req.Muted)
	})
}

func (h *handlers) playPause(c *gin.Context) {
	var req stopReq // device targeting only
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	h.withReceiver(c, req.Device, req.Host, req.Port, func(ctx context.Context, r *cast.Receiver) error {
		return r.PlayPause(ctx)
	})
}

// withReceiver resolves the device, opens a short-lived connection, runs fn,
// and returns 204. Control uses its own connection — never the monitor's
// persistent listener — so the two read models don't collide.
func (h *handlers) withReceiver(c *gin.Context, name, host string, port int, fn func(context.Context, *cast.Receiver) error) {
	dev, err := h.resolveDevice(name, host, port)
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
	if err := fn(ctx, recv); err != nil {
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
			Name:             ch.Name,
			IconURL:          normalizeURL(ch.StationIconURL),
			LiveThumbnailURL: ch.LiveThumbnailURL(),
			NextAirings:      []airingJSON{},
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

func mapDevices(in []state.DeviceStatus, chByVID map[string]string) []deviceJSON {
	out := make([]deviceJSON, 0, len(in))
	for _, ds := range in {
		d := ds.Device
		dj := deviceJSON{Name: d.Name, Host: d.Host, Port: d.Port, UUID: d.UUID, Video: d.IsVideo()}
		if st := ds.Status; st != nil {
			sj := &deviceStatusJSON{
				App:    st.AppName,
				Idle:   st.Idle,
				Volume: st.Volume,
				Muted:  st.Muted,
			}
			if m := st.Media; m != nil {
				sj.PlayerState = m.PlayerState
				sj.Title = m.Title
				sj.Subtitle = m.Subtitle
				sj.ContentID = m.ContentID
				sj.StreamType = m.StreamType
				sj.Live = m.StreamType == "LIVE"
				sj.ImageURL = m.ImageURL
				sj.Channel = chByVID[m.ContentID]
			}
			dj.Status = sj
		}
		out = append(out, dj)
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
