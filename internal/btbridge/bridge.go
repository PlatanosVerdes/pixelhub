// Package btbridge speaks the esp32-divoom TCP protocol to pixelhub and passes its frames to a
// display over a serial Bluetooth link, so a machine's own Bluetooth can stand in for the ESP32.
package btbridge

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"
)

const (
	opConnect = 0x69
	opRelease = 0x96
	linkDown  = 0x96
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

	ops     chan func()
	cl      net.Conn
	lk      io.ReadWriteCloser
	gen     int
	sent    time.Time
	greeted bool
}

func New(dial Dialer, log *slog.Logger) *Bridge {
	return &Bridge{Dial: dial, Log: log, Pace: 15 * time.Millisecond, GreetAfter: 2 * time.Second, Tries: 3, Retry: 2 * time.Second}
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
	default:
		b.forward(msg)
	}
}

func (b *Bridge) connect(mac [6]byte, channel byte) {
	if b.lk != nil {
		b.Log.Info("already connected")
		return
	}
	addr := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
	var err error
	for try := 1; try <= b.Tries; try++ {
		b.Log.Info("connecting", "device", addr, "channel", channel, "attempt", try)
		var lk io.ReadWriteCloser
		if lk, err = b.Dial(mac, channel); err == nil {
			b.linkUp(lk)
			return
		}
		b.Log.Warn("connect failed", "err", err)
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
				}
				b.reply(data)
			}
		}
		if err != nil {
			b.ops <- func() {
				if b.gen == gen && b.lk != nil {
					b.Log.Info("link down", "err", err)
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
	if b.lk != nil {
		b.gen++
		b.lk.Close()
		b.lk = nil
	}
}

func (b *Bridge) reply(data []byte) {
	if b.cl == nil {
		return
	}
	if _, err := b.cl.Write(data); err != nil && !errors.Is(err, net.ErrClosed) {
		b.Log.Warn("client write failed", "err", err)
	}
}
