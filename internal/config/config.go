package config

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/ygelfand/lib-yttv/auth"
)

type Config struct {
	Creds           auth.Creds
	Listen          string
	EPGRefresh      time.Duration
	DiscoverRefresh time.Duration
	DiscoverTimeout time.Duration
	LogLevel        slog.Level
	LogFormat       string
}

func Load() (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("YTTV")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	v.SetDefault("listen", ":8765")
	v.SetDefault("epg_refresh", 5*time.Minute)
	v.SetDefault("discover_refresh", 5*time.Minute)
	v.SetDefault("discover_timeout", 5*time.Second)
	v.SetDefault("log_level", "info")
	v.SetDefault("log_format", "text")

	c := &Config{
		Creds: auth.Creds{
			SAPISID:         v.GetString("sapisid"),
			Secure3PSID:     v.GetString("secure_3psid"),
			GoogleAccountID: v.GetString("google_account_id"),
		},
		Listen:          v.GetString("listen"),
		EPGRefresh:      v.GetDuration("epg_refresh"),
		DiscoverRefresh: v.GetDuration("discover_refresh"),
		DiscoverTimeout: v.GetDuration("discover_timeout"),
		LogFormat:       v.GetString("log_format"),
	}
	if err := c.Creds.Validate(); err != nil {
		return nil, err
	}
	if err := c.LogLevel.UnmarshalText([]byte(strings.ToUpper(v.GetString("log_level")))); err != nil {
		return nil, fmt.Errorf("log_level: %w", err)
	}
	return c, nil
}
