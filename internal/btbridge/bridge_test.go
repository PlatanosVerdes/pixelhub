package btbridge

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"syscall"
	"testing"
	"time"
)

// fakeLink is a display: what the bridge writes lands in written, and display() answers.
type fakeLink struct {
	io.Reader
	w       *io.PipeWriter
	written chan []byte
}

func (f *fakeLink) Write(p []byte) (int, error) {
	f.written <- append([]byte(nil), p...)
	return len(p), nil
}
func (f *fakeLink) Close() error { return f.w.Close() }

func TestBridge(t *testing.T) {
	t.Parallel()
	r, w := io.Pipe()
	link := &fakeLink{Reader: r, w: w, written: make(chan []byte, 8)}
	var dialed [6]byte
	b := New(func(mac [6]byte, _ byte) (io.ReadWriteCloser, error) { dialed = mac; return link, nil }, slog.New(slog.DiscardHandler))
	b.GreetAfter = 50 * time.Millisecond
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go b.Serve(ln)

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	read := func(n int) []byte {
		t.Helper()
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, n)
		if _, err := io.ReadFull(c, buf); err != nil {
			t.Fatal(err)
		}
		return buf
	}

	frame := []byte{0x01, 0x04, 0x00, 0x74, 0x50, 0xc8, 0x00, 0x02}
	c.Write(frame) // before the link: the display is not there
	if got := read(1); got[0] != linkDown {
		t.Fatalf("a frame without a link: %x", got)
	}
	c.Write([]byte{opConnect, 0x01, 0x12, 0x23, 0x45, 0x56, 0x67, 0x01})
	if got := read(len(greeting)); !bytes.Equal(got, greeting) {
		t.Fatalf("no greeting from the display, so the bridge greets: %x", got)
	}
	if dialed != [6]byte{0x01, 0x12, 0x23, 0x45, 0x56, 0x67} {
		t.Errorf("dialed %x", dialed)
	}
	c.Write(append([]byte{0x00}, frame...)) // a stray byte, then a frame
	if got := <-link.written; !bytes.Equal(got, frame) {
		t.Errorf("forwarded %x", got)
	}
	answer := []byte{0x01, 0x06, 0x00, 0x04, 0x32, 0x55, 0x50, 0xe1, 0x00, 0x02}
	go w.Write(answer)
	if got := read(len(answer)); !bytes.Equal(got, answer) {
		t.Errorf("the display's answer: %x", got)
	}
	w.CloseWithError(io.ErrUnexpectedEOF)
	if got := read(1); got[0] != linkDown {
		t.Errorf("the link dropping: %x", got)
	}
}

func TestNewClientDropsTheLink(t *testing.T) {
	t.Parallel()
	dials := 0
	links := make(chan *fakeLink, 2)
	b := New(func([6]byte, byte) (io.ReadWriteCloser, error) {
		dials++
		r, w := io.Pipe()
		l := &fakeLink{Reader: r, w: w, written: make(chan []byte, 8)}
		links <- l
		return l, nil
	}, slog.New(slog.DiscardHandler))
	b.GreetAfter = time.Hour
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go b.Serve(ln)
	connect := []byte{opConnect, 0x01, 0x12, 0x23, 0x45, 0x56, 0x67, 0x01}
	frame := []byte{0x01, 0x04, 0x00, 0x74, 0x50, 0xc8, 0x00, 0x02}

	first, _ := net.Dial("tcp", ln.Addr().String())
	first.Write(connect)
	<-links
	first.Close()

	second, _ := net.Dial("tcp", ln.Addr().String())
	defer second.Close()
	second.Write(frame)
	second.SetReadDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, 1)
	if _, err := io.ReadFull(second, got); err != nil || got[0] != linkDown {
		t.Fatalf("a new client finds the old link dropped: %x %v", got, err)
	}
	second.Write(connect)
	select {
	case <-links:
	case <-time.After(2 * time.Second):
		t.Fatal("the new client could not connect again")
	}
}

func TestBusyDoesNotUseATry(t *testing.T) {
	t.Parallel()
	dials := make(chan int, 8)
	n := 0
	b := New(func([6]byte, byte) (io.ReadWriteCloser, error) {
		n++
		dials <- n
		if n <= 2 {
			return nil, syscall.EBUSY
		}
		r, w := io.Pipe()
		return &fakeLink{Reader: r, w: w, written: make(chan []byte, 8)}, nil
	}, slog.New(slog.DiscardHandler))
	b.Retry, b.GreetAfter = time.Millisecond, time.Hour
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go b.Serve(ln)

	c, _ := net.Dial("tcp", ln.Addr().String())
	defer c.Close()
	c.Write([]byte{opConnect, 0x01, 0x12, 0x23, 0x45, 0x56, 0x67, 0x01})
	for want := 1; want <= 3; want++ {
		select {
		case got := <-dials:
			if got != want {
				t.Fatalf("dial %d, want %d", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("dial %d never came: a busy link used up the one try", want)
		}
	}
}
