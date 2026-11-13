// Package btbridge speaks the esp32-divoom TCP protocol to pixelhub and passes its frames to a
// display over a serial Bluetooth link, so a machine's own Bluetooth can stand in for the ESP32.
package btbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/hfp"
)

const (
	opConnect = 0x69
	opRelease = 0x96
	// opChime asks for the chime on the display's own speaker; the esp32 bridge has no such op.
	opChime = 0x7c
	// opSay carries a sound for that speaker and the faces to paint in time with it, after a four-byte length.
	opSay = 0x7d
	// opSpeaker tells pixelhub, after a talk, whether the display's speaker played it (1) or was not there (0).
	opSpeaker = 0x41
	// opBattery tells pixelhub the display's battery level, 0 (empty) to 9 (full).
	opBattery = 0x42
	maxSay    = 8 << 20
	cmdBright = 0x74
	linkDown  = 0x96
	busyTries = 12
)

// greeting is what the display answers to a design command; pixelhub waits for it before it paints,
// and some displays only send it when asked.
var greeting = []byte{0x01, 0x09, 0x00, 0x04, 0xbd, 0x55, 0x13, 0x01, 0x1e, 0x00, 0x51, 0x01, 0x02}

// Dialer opens the serial channel of the device at mac, in the usual most-significant-first order.
type Dialer func(mac [6]byte, channel byte) (io.ReadWriteCloser, error)

type Bridge struct {
	Dial Dialer
	Log  *slog.Logger
	// Pace is the least time between two frames, as esp32-divoom leaves; GreetAfter is how long
	// to wait for the display's own greeting before sending one.
	Pace, GreetAfter time.Duration
	// Tries is how many times a connect is attempted, Retry the wait in between.
	Tries int
	Retry time.Duration
	// Chime, when set, plays the chime on the speaker of the display at mac.
	Chime func(mac [6]byte)
	// Say, when set, plays wav on that speaker after a wait, until ctx ends; ErrNoSpeaker when the
	// display's speaker is not connected.
	Say func(ctx context.Context, mac [6]byte, wav []byte, after time.Duration) error
	// HandsFree is the display's hands-free channel, 0 for none. While the link is up the bridge holds
	// it open as an iPhone would, which is what makes the display report its battery.
	HandsFree byte
	// HandsFreeAfter is the wait after the link comes up, HandsFreeRetry the wait after hands-free fails
	// or closes; a channel silent for HandsFreeRefresh is reopened, since some displays only report on connect.
	HandsFreeAfter, HandsFreeRetry, HandsFreeRefresh time.Duration

	ops     chan func()
	cl      net.Conn
	lk      io.ReadWriteCloser
	gen     int
	sent    time.Time
	greeted bool
	mac     [6]byte
	talk    *talk
	hfStop  context.CancelFunc
}

// talk is a sound playing with its faces still to paint; anything else painted stops it.
type talk struct {
	stop   context.CancelFunc
	timers []*time.Timer
	// left is the faces still to paint; heard is when the sound has finished.
	left  int
	heard bool
}

func New(dial Dialer, log *slog.Logger) *Bridge {
	// One try: a failed RFCOMM connect takes about 30 s, already as long as pixelhub waits for the
	// greeting, and pixelhub retries with its own backoff; piled-up tries switched the display off.
	return &Bridge{
		Dial: dial, Log: log, Pace: 15 * time.Millisecond, GreetAfter: 2 * time.Second, Tries: 1, Retry: 2 * time.Second,
		HandsFreeAfter: 5 * time.Second, HandsFreeRetry: 5 * time.Minute, HandsFreeRefresh: 15 * time.Minute,
	}
}

// Serve takes one client at a time from ln, a new one replacing the last, until ln closes.
func (b *Bridge) Serve(ln net.Listener) error {
	b.ops = make(chan func(), 64)
	go func() {
		for op := range b.ops {
			op()
		}
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		b.ops <- func() { b.accept(c) }
	}
}

func (b *Bridge) accept(c net.Conn) {
	if b.cl != nil {
		b.cl.Close()
	}
	// A client comes back after the display stopped answering; a link kept open can look up while the
	// display no longer listens, so every client starts from a fresh one.
	if b.lk != nil {
		b.Log.Info("new client, dropping the old link")
		b.close()
	}
	b.cl = c
	b.Log.Info("client connected", "from", c.RemoteAddr())
	go b.readClient(c)
}

func (b *Bridge) readClient(c net.Conn) {
	buf := make([]byte, 4096)
	var in []byte
	for {
		n, err := c.Read(buf)
		in = append(in, buf[:n]...)
		for {
			msg, rest, ok := next(in)
			if !ok {
				break
			}
			in = rest
			if msg != nil {
				b.ops <- func() { b.handle(c, msg) }
			}
		}
		if err != nil {
			b.ops <- func() {
				if b.cl == c {
					b.cl = nil
					b.Log.Info("client gone")
				}
			}
			return
		}
	}
}

// next splits off the first message: a connect, a release or a frame; a nil message with ok is a
// stray byte to skip, and !ok means more bytes are needed.
func next(in []byte) (msg, rest []byte, ok bool) {
	if len(in) == 0 {
		return nil, in, false
	}
	size := 0
	switch in[0] {
	case opConnect:
		size = 8
	case opRelease:
		size = 7
	case opChime:
		size = 1
	case opSay:
		if len(in) < 5 {
			return nil, in, false
		}
		n := int(binary.LittleEndian.Uint32(in[1:5]))
		if n > maxSay {
			return nil, in[1:], true
		}
		size = 5 + n
	case 0x01:
		if len(in) < 3 {
			return nil, in, false
		}
		size = int(in[1]) | int(in[2])<<8 + 4
	default:
		return nil, in[1:], true
	}
	if len(in) < size {
		return nil, in, false
	}
	return in[:size:size], in[size:], true
}

func (b *Bridge) handle(c net.Conn, msg []byte) {
	if c != b.cl {
		return
	}
	switch msg[0] {
	case opConnect:
		var mac [6]byte
		copy(mac[:], msg[1:7])
		b.connect(mac, msg[7])
	case opRelease:
		b.Log.Info("release")
		b.close()
	case opChime:
		if b.Chime == nil || b.lk == nil {
			b.Log.Info("chime skipped", "linked", b.lk != nil)
			return
		}
		mac := b.mac
		go b.Chime(mac)
	case opSay:
		b.say(msg[5:])
	default:
		if msg[0] == 0x01 && len(msg) > 3 && msg[3] != cmdBright && b.talk != nil {
			b.stopTalk()
		}
		b.forward(msg)
	}
}

// say plays the sound and paints each face at its time, both from now, so neither drifts from the other.
func (b *Bridge) say(body []byte) {
	b.stopTalk()
	if b.Say == nil || b.lk == nil {
		b.Log.Info("talk skipped", "linked", b.lk != nil)
		return
	}
	ms := func(p []byte) time.Duration { return time.Duration(binary.LittleEndian.Uint16(p)) * time.Millisecond }
	if len(body) < 8 {
		b.Log.Warn("talk too short")
		return
	}
	soundAfter, at := ms(body[0:]), ms(body[2:])
	n := int(binary.LittleEndian.Uint32(body[4:8]))
	if len(body) < 8+n {
		b.Log.Warn("talk sound cut short")
		return
	}
	wav, rest := body[8:8+n], body[8+n:]
	ctx, stop := context.WithCancel(context.Background())
	t := &talk{stop: stop}
	gen := b.gen
	for len(rest) >= 4 {
		hold, size := ms(rest), int(binary.LittleEndian.Uint16(rest[2:4]))
		if len(rest) < 4+size {
			break
		}
		face := rest[4 : 4+size]
		t.left++
		t.timers = append(t.timers, time.AfterFunc(at, func() {
			b.ops <- func() {
				if b.talk == t && b.gen == gen {
					b.forward(face)
					t.left--
					b.endTalk(t)
				}
			}
		}))
		at += hold
		rest = rest[4+size:]
	}
	b.talk = t
	mac := b.mac
	go func() {
		err := b.Say(ctx, mac, wav, soundAfter)
		b.ops <- func() {
			t.heard = true
			b.endTalk(t)
			switch {
			case err == nil:
				b.reply([]byte{opSpeaker, 1})
			case errors.Is(err, ErrNoSpeaker):
				b.reply([]byte{opSpeaker, 0})
			}
		}
	}()
}

// endTalk lets go of a talk whose sound and faces are done.
func (b *Bridge) endTalk(t *talk) {
	if b.talk == t && t.left == 0 && t.heard {
		b.talk = nil
	}
}

func (b *Bridge) stopTalk() {
	if b.talk == nil {
		return
	}
	b.talk.stop()
	for _, t := range b.talk.timers {
		t.Stop()
	}
	b.talk = nil
}

func (b *Bridge) connect(mac [6]byte, channel byte) {
	if b.lk != nil {
		b.Log.Info("already connected")
		return
	}
	addr := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
	var err error
	busy := 0
	for try := 1; try <= b.Tries; try++ {
		b.Log.Info("connecting", "device", addr, "channel", channel, "attempt", try)
		var lk io.ReadWriteCloser
		if lk, err = b.Dial(mac, channel); err == nil {
			b.mac = mac
			b.linkUp(lk)
			return
		}
		b.Log.Warn("connect failed", "err", err)
		// The kernel takes about 20 s to close the last link; busy fails at once, so it costs no try.
		// 12 tries 2 s apart still leave a link up inside pixelhub's 30 s greeting wait.
		if errors.Is(err, syscall.EBUSY) && busy < busyTries {
			busy++
			try--
		}
		if try < b.Tries {
			time.Sleep(b.Retry)
		}
	}
	b.reply([]byte{linkDown})
}

func (b *Bridge) linkUp(lk io.ReadWriteCloser) {
	b.lk, b.greeted = lk, false
	b.gen++
	gen := b.gen
	b.Log.Info("link up")
	go b.readLink(lk, gen)
	if b.HandsFree != 0 {
		ctx, stop := context.WithCancel(context.Background())
		b.hfStop = stop
		go b.handsFree(ctx, b.mac, gen)
	}
	time.AfterFunc(b.GreetAfter, func() {
		b.ops <- func() {
			if b.gen == gen && b.lk != nil && !b.greeted {
				b.greeted = true
				b.reply(greeting)
			}
		}
	})
}

func (b *Bridge) readLink(lk io.ReadWriteCloser, gen int) {
	buf := make([]byte, 1024)
	for {
		n, err := lk.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			b.ops <- func() {
				if b.gen != gen {
					return
				}
				if len(data) > 4 && data[3] == 0x04 && data[4] == 0xbd {
					b.greeted = true
					// The display greets each face a talk paints; pixelhub would take it for a restart and repaint.
					if b.talk != nil {
						return
					}
				}
				b.reply(data)
			}
		}
		if err != nil {
			b.ops <- func() {
				if b.gen == gen && b.lk != nil {
					b.Log.Info("link down", "err", err)
					b.stopTalk()
					b.stopHandsFree()
					b.lk.Close()
					b.lk = nil
					b.reply([]byte{linkDown})
				}
			}
			return
		}
	}
}

func (b *Bridge) forward(frame []byte) {
	if b.lk == nil {
		b.reply([]byte{linkDown})
		return
	}
	if wait := b.Pace - time.Since(b.sent); wait > 0 {
		time.Sleep(wait)
	}
	b.sent = time.Now()
	if _, err := b.lk.Write(frame); err != nil {
		b.Log.Warn("write failed", "err", err)
		b.close()
		b.reply([]byte{linkDown})
	}
}

func (b *Bridge) close() {
	b.stopTalk()
	b.stopHandsFree()
	if b.lk != nil {
		b.gen++
		b.lk.Close()
		b.lk = nil
	}
}

func (b *Bridge) stopHandsFree() {
	if b.hfStop != nil {
		b.hfStop()
		b.hfStop = nil
	}
}

// handsFree keeps the display's hands-free channel open until ctx ends, passing on each battery level.
// Failed connects wait minutes, not seconds: piled-up Bluetooth connects have switched the display off.
func (b *Bridge) handsFree(ctx context.Context, mac [6]byte, gen int) {
	wait := b.HandsFreeAfter
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = b.HandsFreeRetry
		hf, err := b.Dial(mac, b.HandsFree)
		if err != nil {
			b.Log.Info("hands-free not opened", "err", err, "retry_in", wait.String())
			continue
		}
		b.Log.Info("hands-free open")
		err = b.serveHandsFree(ctx, hf, gen)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errQuiet) {
			wait = b.HandsFreeAfter
		}
		b.Log.Info("hands-free closed", "err", err, "retry_in", wait.String())
	}
}

func (b *Bridge) serveHandsFree(ctx context.Context, hf io.ReadWriteCloser, gen int) error {
	stop := context.AfterFunc(ctx, func() { hf.Close() })
	defer stop()
	var quiet atomic.Bool
	reopen := time.AfterFunc(b.HandsFreeRefresh, func() {
		quiet.Store(true)
		hf.Close()
	})
	defer reopen.Stop()
	defer hf.Close()
	err := hfp.Serve(hf, time.Now, func(level int) {
		reopen.Reset(b.HandsFreeRefresh)
		b.ops <- func() {
			if b.gen == gen {
				b.Log.Info("battery", "level", level)
				b.reply([]byte{opBattery, byte(level)})
			}
		}
	})
	if quiet.Load() {
		return errQuiet
	}
	return err
}

// errQuiet is a hands-free channel closed on purpose after a silent while, to be reopened at once.
var errQuiet = errors.New("no battery level for a while, reopening")

// ErrNoSpeaker is a sound with nowhere to play: the display is linked but its speaker is not.
var ErrNoSpeaker = errors.New("the display's speaker is not connected")

func (b *Bridge) reply(data []byte) {
	if b.cl == nil {
		return
	}
	if _, err := b.cl.Write(data); err != nil && !errors.Is(err, net.ErrClosed) {
		b.Log.Warn("client write failed", "err", err)
	}
}
