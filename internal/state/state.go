package state

import (
	"context"
	"log/slog"
	"sort"
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
	devices  map[string]*deviceEntry // keyed by cast.Device.ID()
	changed  chan struct{}           // closed+replaced on any change; wakes /events
}

// deviceEntry is a discovered device plus, for video devices, its live status
// (pushed by a persistent cast listener) and the cancel for that listener.
type deviceEntry struct {
	dev    cast.Device
	status *cast.Status
	cancel context.CancelFunc
}

// DeviceStatus pairs a device with its latest status (nil when unknown/idle).
type DeviceStatus struct {
	Device cast.Device
	Status *cast.Status
}

func New(session *yttv.Session) *Store {
	return &Store{
		Session: session,
		devices: map[string]*deviceEntry{},
		changed: make(chan struct{}),
	}
}

// Changed returns a channel that is closed on the next state change. Callers
// re-fetch it after each wake. No registration/cleanup needed: a change closes
// the current channel (waking everyone) and installs a fresh one.
func (s *Store) Changed() <-chan struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.changed
}

// broadcast wakes all Changed() waiters. Caller must hold s.mu for writing.
func (s *Store) broadcast() {
	close(s.changed)
	s.changed = make(chan struct{})
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
	out := make([]cast.Device, 0, len(s.devices))
	for _, e := range s.devices {
		out = append(out, e.dev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// DeviceStatuses returns each device with its latest status, sorted by name.
func (s *Store) DeviceStatuses() []DeviceStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DeviceStatus, 0, len(s.devices))
	for _, e := range s.devices {
		out = append(out, DeviceStatus{Device: e.dev, Status: e.status})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Device.Name < out[j].Device.Name })
	return out
}

func (s *Store) FindDevice(nameSubstr string) (cast.Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	want := strings.ToLower(nameSubstr)
	for _, e := range s.devices {
		if strings.Contains(strings.ToLower(e.dev.Name), want) {
			return e.dev, true
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

// Run starts the EPG refresh loop and the continuous device monitor.
func (s *Store) Run(ctx context.Context, epgEvery, discEvery, discTimeout time.Duration) {
	go s.loop(ctx, "epg", epgEvery, func(c context.Context) error {
		return s.refreshChannels(c)
	})
	go s.monitorDevices(ctx, discEvery, discTimeout)
}

// monitorDevices consumes discovery events: it tracks the device list and, for
// each video device, holds a persistent cast listener that pushes live status.
func (s *Store) monitorDevices(ctx context.Context, interval, window time.Duration) {
	for ev := range discover.Watch(ctx, interval, window) {
		if ev.Up {
			s.deviceUp(ctx, ev.Device)
		} else {
			s.deviceDown(ev.Device)
		}
	}
}

func (s *Store) deviceUp(ctx context.Context, d cast.Device) {
	id := d.ID()
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.broadcast()
	if e, ok := s.devices[id]; ok {
		e.dev = d // refresh metadata (e.g. IP change)
		return
	}
	e := &deviceEntry{dev: d}
	s.devices[id] = e
	if d.IsVideo() {
		dctx, cancel := context.WithCancel(ctx)
		e.cancel = cancel
		go s.watchStatus(dctx, id, d)
	}
	slog.InfoContext(ctx, "device up", "name", d.Name, "video", d.IsVideo())
}

func (s *Store) deviceDown(d cast.Device) {
	id := d.ID()
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.broadcast()
	if e, ok := s.devices[id]; ok {
		if e.cancel != nil {
			e.cancel()
		}
		delete(s.devices, id)
		slog.Info("device down", "name", d.Name)
	}
}

func (s *Store) watchStatus(ctx context.Context, id string, d cast.Device) {
	for st := range cast.WatchDevice(ctx, d) {
		s.mu.Lock()
		if e, ok := s.devices[id]; ok {
			e.status = st
		}
		s.broadcast()
		s.mu.Unlock()
	}
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
