package api

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	sloggin "github.com/samber/slog-gin"
	"github.com/ygelfand/yttv-bridge/internal/state"
)

func Router(store *state.Store) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(sloggin.New(slog.Default()), gin.Recovery())

	h := &handlers{store: store}
	r.GET("/healthz", func(c *gin.Context) { c.String(200, "ok") })
	r.GET("/channels", h.channels)
	r.GET("/devices", h.devices)
	r.GET("/snapshot", h.snapshot)
	r.POST("/cast", h.cast)
	r.POST("/stop", h.stop)
	r.POST("/volume", h.volume)
	r.POST("/mute", h.mute)
	r.POST("/playpause", h.playPause)
	return r
}
