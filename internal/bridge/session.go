package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/divoom"
)

const (
	opConnect  = 0x69
	opRelease  = 0x96
	opLinkDown = 0x96
)

// A link must stay up this long before a drop counts as new trouble rather than the same flapping.
const stableLink = time.Minute

const probeAttempts = 3

var (
	errTimeout  = errors.New("timed out")
	errReleased = errors.New("released")
	errLinkLost = errors.New("bridge reported the Bluetooth link down")
)

type Options struct {
	Address         string
	MAC             net.HardwareAddr
	Channel         byte
	DialTimeout     time.Duration
	WriteTimeout    time.Duration
	ProbeTimeout    time.Duration
	GreetingTimeout time.Duration
	Keepalive       time.Duration
	MinBackoff      time.Duration
	MaxBackoff      time.Duration
	ChunkPause      time.Duration
}

func DefaultOptions(address string, mac net.HardwareAddr, channel byte) Options {
	return Options{
		Address:         address,
		MAC:             mac,
		Channel:         channel,
		DialTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		ProbeTimeout:    5 * time.Second,
		GreetingTimeout: 30 * time.Second,
		Keepalive:       30 * time.Second,
		MinBackoff:      2 * time.Second,
		MaxBackoff:      60 * time.Second,
		ChunkPause:      50 * time.Millisecond,
	}
}

// Session owns the TCP connection to an esp32-divoom bridge and the Bluetooth link behind it.
// Only Run's goroutine touches the connection.
type Session struct {
	opt     Options
	log     *slog.Logger
	paint   chan struct{}
	control chan struct{}

	mu          sync.Mutex
	desired     display.Content
	version     int
	released    bool
	releaseSent bool
	status      display.Status
}

var _ display.Display = (*Session)(nil)

func New(opt Options, log *slog.Logger) *Session {
	return &Session{
		opt:     opt,
		log:     log,
		paint:   make(chan struct{}, 1),
		control: make(chan struct{}, 1),
		status:  display.Status{Address: opt.Address},
	}
}

func (s *Session) Show(c display.Content) {
	s.mu.Lock()
	s.desired = c
	s.version++
	s.mu.Unlock()
	poke(s.paint)
}

func (s *Session) Release() {
	s.mu.Lock()
	s.released, s.releaseSent = true, false
	s.mu.Unlock()
	poke(s.control)
}

func (s *Session) Claim() {
	s.mu.Lock()
	s.released = false
	s.mu.Unlock()
	poke(s.control)
}

func (s *Session) Status() display.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// WaitConnected blocks until the Evo has greeted the bridge.
func (s *Session) WaitConnected(ctx context.Context) error {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for !s.Status().Connected {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return nil
}

// Run waits for the first Show, since probing sends the brightness of what should be on screen.
func (s *Session) Run(ctx context.Context) {
	for _, version := s.wanted(); version == 0; _, version = s.wanted() {
		select {
		case <-ctx.Done():
			return
		case <-s.paint:
		}
	}
	backoff := s.opt.MinBackoff
	for ctx.Err() == nil {
		if s.isReleased() {
			if !s.sendPendingRelease(ctx) {
				s.pause(ctx, backoff)
				backoff = min(backoff*2, s.opt.MaxBackoff)
				continue
			}
			select {
			case <-ctx.Done():
			case <-s.control:
			}
			continue
		}
		upSince, err := s.serve(ctx)
		s.setConnected(false)
		if ctx.Err() != nil || errors.Is(err, errReleased) {
			continue
		}
		if !upSince.IsZero() && time.Since(upSince) >= stableLink {
			backoff = s.opt.MinBackoff
		}
		s.log.Warn("bridge session down", "err", err, "retry_in", backoff.String())
		s.pause(ctx, backoff)
		backoff = min(backoff*2, s.opt.MaxBackoff)
	}
}

func (s *Session) pause(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			return
		case <-s.control:
			if s.isReleased() {
				return
			}
		}
	}
}

// serve holds one TCP connection until it or the Bluetooth link fails; upSince is zero if the Evo never answered.
func (s *Session) serve(ctx context.Context) (upSince time.Time, err error) {
	conn, err := s.dial(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer conn.Close()
	events := make(chan event, 16)
	done := make(chan struct{})
	defer close(done)
	go read(conn, events, done)

	link := &link{Session: s, ctx: ctx, conn: conn, events: events}
	up, err := link.probe()
	if err != nil {
		return time.Time{}, err
	}
	if !up {
		s.log.Info("connecting the Evo", "mac", s.opt.MAC.String())
		if err := link.connect(); err != nil {
			return time.Time{}, err
		}
	}
	s.setConnected(true)
	s.log.Info("Evo connected", "already", up)
	return time.Now(), link.hold()
}

type link struct {
	*Session
	ctx     context.Context
	conn    net.Conn
	events  chan event
	painted int
	shown   display.Content
}

// probe never reads silence as "not connected": the bridge always answers, and connecting while the
// link is up makes it redial and the Evo refuse. A bridge that stays silent gets a new TCP connection.
func (l *link) probe() (bool, error) {
	for range probeAttempts {
		if err := l.ping(); err != nil {
			return false, err
		}
		ev, err := l.next(time.Now().Add(l.opt.ProbeTimeout))
		switch {
		case errors.Is(err, errTimeout):
			continue
		case err != nil:
			return false, err
		}
		return !ev.lost, nil
	}
	return false, errors.New("no answer to the probe")
}

func (l *link) connect() error {
	msg := append([]byte{opConnect}, l.opt.MAC...)
	if err := l.write(append(msg, l.opt.Channel)); err != nil {
		return err
	}
	deadline := time.Now().Add(l.opt.GreetingTimeout)
	for {
		ev, err := l.next(deadline)
		if err != nil {
			return fmt.Errorf("wait for the Evo greeting: %w", err)
		}
		// A stale link-down byte here must not end the wait: redialling mid-connect makes the Evo refuse.
		if !ev.lost && divoom.IsGreeting(ev.frame) {
			return nil
		}
	}
}

func (l *link) hold() error {
	keepalive := time.NewTicker(l.opt.Keepalive)
	defer keepalive.Stop()
	// A weak Bluetooth signal answers seconds late, so a ping has until the next one to be answered.
	waiting := false
	for {
		if err := l.repaint(); err != nil {
			return err
		}
		select {
		case <-l.ctx.Done():
			return nil
		case <-l.control:
			if l.isReleased() {
				return l.release()
			}
		case <-l.paint:
		case <-keepalive.C:
			if waiting {
				return errors.New("no answer to the keepalive")
			}
			if err := l.ping(); err != nil {
				return err
			}
			waiting = true
		case ev := <-l.events:
			if ev.err != nil {
				return fmt.Errorf("read bridge: %w", ev.err)
			}
			if ev.lost {
				return errLinkLost
			}
			if ev.battery > 0 || ev.light > 0 || ev.sound {
				l.sensed(ev)
				continue
			}
			l.acked()
			waiting = false
			if v, ok := divoom.ParseState(ev.frame); ok {
				l.reported(v)
			}
			if divoom.IsGreeting(ev.frame) {
				l.painted, l.shown = 0, display.Content{}
			}
		}
	}
}

// next returns the next event before deadline; a release while waiting is sent at once.
func (l *link) next(deadline time.Time) (event, error) {
	t := time.NewTimer(time.Until(deadline))
	defer t.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return event{}, l.ctx.Err()
		case <-t.C:
			return event{}, errTimeout
		case <-l.control:
			if l.isReleased() {
				return event{}, l.release()
			}
		case ev := <-l.events:
			if ev.err != nil {
				return event{}, fmt.Errorf("read bridge: %w", ev.err)
			}
			if ev.battery > 0 || ev.light > 0 || ev.sound {
				l.sensed(ev)
				continue
			}
			if !ev.lost {
				l.acked()
			}
			return ev, nil
		}
	}
}

func (l *link) repaint() error {
	content, version := l.wanted()
	if version == l.painted {
		return nil
	}
	l.painted = version
	msgs := [][]byte{divoom.Brightness(content.Brightness)}
	var err error
	// Native modes are sent again to keep the clock right, but a tool sent again would restart.
	resend := content.Kind != display.Picture && !content.View.Mode.Tool()
	if resend || !content.SamePicture(l.shown) {
		msgs, err = divoom.Encode(content)
		msgs = append(divoom.Leave(l.shown, content), msgs...)
	}
	l.shown = content
	if err != nil {
		l.log.Warn("content skipped", "content", content.Name, "err", err)
		return nil
	}
	for i, msg := range msgs {
		if i > 0 {
			time.Sleep(l.opt.ChunkPause)
		}
		if err := l.write(msg); err != nil {
			return err
		}
	}
	return nil
}

func (l *link) release() error {
	msg := append([]byte{opRelease}, l.opt.MAC...)
	if err := l.write(msg); err != nil {
		return err
	}
	l.mu.Lock()
	l.releaseSent = true
	l.mu.Unlock()
	l.log.Info("Evo released")
	return errReleased
}

func (l *link) write(msg []byte) error {
	if err := l.conn.SetWriteDeadline(time.Now().Add(l.opt.WriteTimeout)); err != nil {
		return fmt.Errorf("write bridge: %w", err)
	}
	if _, err := l.conn.Write(msg); err != nil {
		return fmt.Errorf("write bridge: %w", err)
	}
	return nil
}

// sendPendingRelease covers a release that came while no connection was open: the bridge may still
// hold the Evo. It reports false when the bridge could not be told and it should be tried again.
func (s *Session) sendPendingRelease(ctx context.Context) bool {
	s.mu.Lock()
	sent := s.releaseSent
	s.mu.Unlock()
	if sent {
		return true
	}
	conn, err := s.dial(ctx)
	if err != nil {
		s.log.Warn("release not sent", "err", err)
		return false
	}
	defer conn.Close()
	l := &link{Session: s, ctx: ctx, conn: conn}
	if err := l.release(); !errors.Is(err, errReleased) {
		s.log.Warn("release not sent", "err", err)
		return false
	}
	return true
}

func (s *Session) dial(ctx context.Context) (net.Conn, error) {
	conn, err := (&net.Dialer{Timeout: s.opt.DialTimeout}).DialContext(ctx, "tcp", s.opt.Address)
	if err != nil {
		return nil, fmt.Errorf("dial bridge: %w", err)
	}
	return conn, nil
}

func (s *Session) wanted() (display.Content, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.desired, s.version
}

// ping doubles as the probe: the brightness already on screen changes nothing, and the answer tells if the link is up.
func (l *link) ping() error {
	content, _ := l.wanted()
	return l.write(divoom.Brightness(content.Brightness))
}

func (s *Session) isReleased() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.released
}

func (s *Session) setConnected(up bool) {
	s.mu.Lock()
	s.status.Connected = up
	if !up {
		s.status.Device = nil
	}
	s.mu.Unlock()
}

func (s *Session) sensed(ev event) {
	if ev.battery > 0 {
		s.batteryReported(ev.battery - 1)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.light > 0 {
		light := ev.light - 1
		s.status.Light, s.status.LightAt = &light, time.Now()
	}
	if ev.sound {
		s.status.SoundAt = time.Now()
	}
}

func (s *Session) batteryReported(level int) {
	s.mu.Lock()
	s.status.Battery, s.status.BatteryAt = &level, time.Now()
	s.mu.Unlock()
	s.log.Info("battery reported", "level", level)
}

func (s *Session) reported(v display.View) {
	s.mu.Lock()
	s.status.Device = &v
	s.mu.Unlock()
}

func (s *Session) acked() {
	s.mu.Lock()
	s.status.Acks++
	s.status.LastAck = time.Now()
	s.mu.Unlock()
}

func read(conn net.Conn, events chan<- event, done <-chan struct{}) {
	var p parser
	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		for _, ev := range p.feed(buf[:n]) {
			select {
			case events <- ev:
			case <-done:
				return
			}
		}
		if err != nil {
			select {
			case events <- event{err: err}:
			case <-done:
			}
			return
		}
	}
}

func poke(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
