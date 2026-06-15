package state

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	yttv "github.com/ygelfand/lib-yttv"
	"github.com/ygelfand/lib-yttv/cast"
	"github.com/ygelfand/lib-yttv/discover"
	"github.com/ygelfand/lib-yttv/epg"
)

type Store struct {
	Session *yttv.Session

	mu       sync.RWMutex
	channels []epg.Channel
	devices  []cast.Device
}

func New(session *yttv.Session) *Store {
	return &Store{Session: session}
}

func (s *Store) Channels() []epg.Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]epg.Channel, len(s.channels))
	copy(out, s.channels)
	return out
}

func (s *Store) Devices() []cast.Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]cast.Device, len(s.devices))
	copy(out, s.devices)
	return out
}

func (s *Store) FindDevice(nameSubstr string) (cast.Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	want := strings.ToLower(nameSubstr)
	for _, d := range s.devices {
		if strings.Contains(strings.ToLower(d.Name), want) {
			return d, true
		}
	}
	return cast.Device{}, false
}

func (s *Store) refreshChannels(ctx context.Context) error {
	chs, err := s.Session.Channels(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.channels = chs
	s.mu.Unlock()
	return nil
}

func (s *Store) refreshDevices(ctx context.Context, timeout time.Duration) error {
	devs, err := discover.Discover(ctx, timeout)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.devices = devs
	s.mu.Unlock()
	return nil
}

func (s *Store) Run(ctx context.Context, epgEvery, discEvery, discTimeout time.Duration) {
	go s.loop(ctx, "epg", epgEvery, func(c context.Context) error {
		return s.refreshChannels(c)
	})
	go s.loop(ctx, "discover", discEvery, func(c context.Context) error {
		return s.refreshDevices(c, discTimeout)
	})
}

func (s *Store) loop(ctx context.Context, name string, every time.Duration, fn func(context.Context) error) {
	run := func() {
		c, cancel := context.WithTimeout(ctx, every)
		defer cancel()
		if err := fn(c); err != nil {
			slog.WarnContext(ctx, "refresh failed", "cache", name, "err", err)
		}
	}
	run()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
