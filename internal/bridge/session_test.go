package bridge

import (
	"context"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/divoom"
)

var evoMAC = net.HardwareAddr{0x01, 0x12, 0x23, 0x45, 0x56, 0x67}

// fakeBridge answers like the esp32 bridge: acks while the Evo is linked, 0x96 while it is not.
// Ops it receives are reported as "connect", "release", "frame:<cmd>" or "dropped:<cmd>" (sent while unlinked).
type fakeBridge struct {
	t          *testing.T
	ln         net.Listener
	ops        chan string
	greetDelay time.Duration

	mu      sync.Mutex
	linked  bool
	accepts int
	conns   []net.Conn
}

func newFakeBridge(t *testing.T, linked bool) *fakeBridge {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeBridge{t: t, ln: ln, ops: make(chan string, 256), linked: linked, greetDelay: 50 * time.Millisecond}
	t.Cleanup(func() {
		ln.Close()
		f.drop()
	})
	go f.accept()
	return f
}

func (f *fakeBridge) accept() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.accepts++
		f.conns = append(f.conns, conn)
		f.mu.Unlock()
		go f.serve(conn)
	}
}

func (f *fakeBridge) serve(conn net.Conn) {
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := conn.Read(tmp)
		buf = append(buf, tmp[:n]...)
		for {
			size := opSize(buf)
			if size == 0 || len(buf) < size {
				break
			}
			f.handle(conn, buf[:size])
			buf = buf[size:]
		}
		if err != nil {
			return
		}
	}
}

func opSize(buf []byte) int {
	switch {
	case len(buf) == 0:
		return 0
	case buf[0] == opConnect:
		return 8
	case buf[0] == opRelease:
		return 7
	default:
		return divoom.FrameLen(buf)
	}
}

func (f *fakeBridge) handle(conn net.Conn, op []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch op[0] {
	case opConnect:
		f.ops <- "connect"
		go func() {
			time.Sleep(f.greetDelay)
			f.mu.Lock()
			f.linked = true
			f.mu.Unlock()
			conn.Write(mustHex(greetingHex))
		}()
	case opRelease:
		f.ops <- "release"
		f.linked = false
	default:
		cmd := op[3]
		if !f.linked {
			f.ops <- fmt.Sprintf("dropped:%02x", cmd)
			conn.Write([]byte{opLinkDown})
			return
		}
		f.ops <- fmt.Sprintf("frame:%02x", cmd)
		conn.Write(divoom.Message(0x04, []byte{cmd, 0x55, 0x00}))
	}
}

func (f *fakeBridge) drop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		c.Close()
	}
	f.conns = nil
}

func (f *fakeBridge) loseLink() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.linked = false
	for _, c := range f.conns {
		c.Write([]byte{opLinkDown})
	}
}

func (f *fakeBridge) acceptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.accepts
}

func (f *fakeBridge) expect(want ...string) {
	f.t.Helper()
	for _, w := range want {
		select {
		case got := <-f.ops:
			if got != w {
				f.t.Fatalf("bridge got %s, want %s", got, w)
			}
		case <-time.After(3 * time.Second):
			f.t.Fatalf("bridge never got %s", w)
		}
	}
}

func (f *fakeBridge) expectQuiet(d time.Duration) {
	f.t.Helper()
	select {
	case got := <-f.ops:
		f.t.Fatalf("bridge got %s, want nothing", got)
	case <-time.After(d):
	}
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func startSession(t *testing.T, f *fakeBridge, show *display.Content) *Session {
	t.Helper()
	opt := DefaultOptions(f.ln.Addr().String(), evoMAC, 1)
	opt.ProbeTimeout = 500 * time.Millisecond
	opt.GreetingTimeout = time.Second
	opt.Keepalive = time.Hour
	opt.MinBackoff = 10 * time.Millisecond
	opt.MaxBackoff = 20 * time.Millisecond
	opt.ChunkPause = time.Millisecond
	s := New(opt, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if show != nil {
		s.Show(*show)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return s
}

func picture(frames int) display.Content {
	c := display.Content{Kind: display.Picture, Name: fmt.Sprintf("%d frames", frames), Brightness: 40}
	for range frames {
		c.Frames = append(c.Frames, display.Frame{Image: image.NewNRGBA(image.Rect(0, 0, 16, 16)), Delay: 100 * time.Millisecond})
	}
	return c
}

func TestAlreadyLinkedSkipsConnect(t *testing.T) {
	t.Parallel()
	f := newFakeBridge(t, true)
	s := startSession(t, f, new(picture(1)))
	f.expect("frame:74", "frame:44", "frame:74")
	s.Show(picture(2))
	f.expect("frame:49", "frame:74")
	s.Show(picture(2).WithBrightness(90))
	f.expect("frame:74")
	f.expectQuiet(100 * time.Millisecond)
	if st := s.Status(); !st.Connected || st.Acks < 4 || st.LastAck.IsZero() {
		t.Errorf("status %+v, want connected with acks counted", st)
	}
}

func TestConnectWaitsForGreeting(t *testing.T) {
	t.Parallel()
	f := newFakeBridge(t, false)
	f.greetDelay = 300 * time.Millisecond
	startSession(t, f, new(picture(1)))
	f.expect("dropped:74", "connect", "frame:44", "frame:74")
}

func TestReconnects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		fail  func(*fakeBridge)
		after []string
	}{
		{"tcp dropped", (*fakeBridge).drop, []string{"frame:74", "frame:44", "frame:74"}},
		{"bluetooth lost", (*fakeBridge).loseLink, []string{"dropped:74", "connect", "frame:44", "frame:74"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFakeBridge(t, true)
			startSession(t, f, new(picture(1)))
			f.expect("frame:74", "frame:44", "frame:74")
			tt.fail(f)
			f.expect(tt.after...)
			if n := f.acceptCount(); n != 2 {
				t.Errorf("%d connections, want 2", n)
			}
		})
	}
}

func TestReleaseAndClaim(t *testing.T) {
	t.Parallel()
	f := newFakeBridge(t, true)
	s := startSession(t, f, new(picture(1)))
	f.expect("frame:74", "frame:44", "frame:74")

	s.Release()
	f.expect("release")
	f.drop()
	f.expectQuiet(200 * time.Millisecond)
	if n := f.acceptCount(); n != 1 {
		t.Errorf("%d connections while released, want 1", n)
	}
	if st := s.Status(); st.Connected || !s.isReleased() {
		t.Errorf("status %+v, want released and not connected", st)
	}

	s.Show(display.ClockContent().WithBrightness(20))
	s.Claim()
	f.expect("dropped:74", "connect", "frame:45", "frame:74")
	if s.isReleased() {
		t.Error("still released after the claim")
	}
}

func TestReleaseWhileConnecting(t *testing.T) {
	t.Parallel()
	f := newFakeBridge(t, false)
	f.greetDelay = time.Hour
	s := startSession(t, f, new(picture(1)))
	f.expect("dropped:74", "connect")
	s.Release()
	f.expect("release")
	f.expectQuiet(200 * time.Millisecond)
}

func TestUnpromptedGreetingRepaints(t *testing.T) {
	t.Parallel()
	f := newFakeBridge(t, true)
	startSession(t, f, new(picture(1)))
	f.expect("frame:74", "frame:44", "frame:74")
	f.mu.Lock()
	for _, c := range f.conns {
		c.Write(mustHex(greetingHex))
	}
	f.mu.Unlock()
	f.expect("frame:44", "frame:74")
}

func TestNothingSentBeforeTheFirstShow(t *testing.T) {
	t.Parallel()
	f := newFakeBridge(t, true)
	s := startSession(t, f, nil)
	f.expectQuiet(200 * time.Millisecond)
	s.Show(picture(1))
	f.expect("frame:74", "frame:44", "frame:74")
}
