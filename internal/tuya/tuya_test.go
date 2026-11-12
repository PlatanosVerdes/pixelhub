package tuya

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash/crc32"
	"net"
	"strings"
	"testing"
	"time"
)

const (
	testKey = "0123456789abcdef"
	testID  = "bf0123"
)

// Vectors built with openssl enc -aes-128-ecb and Python's zlib.crc32, not with this package.
const (
	controlOnHex = "000055aa000000010000000700000067332e33000000000000000000000000cf86245f80a43fd581e9f874f8bade177594d631cf3172d0a76ee240a66d4e27a73fe68f92981e325911d52ad665ec472125e1846bd4f824d1b2e468c239926631836d84bd28cb4b20619eb84882ca2a5da9e84d0000aa55"
	queryHex     = "000055aa000000020000000a0000005888c961f323a7583de3767fe34c87aaf2e064714545cf35869d3f50aa92d975727594d631cf3172d0a76ee240a66d4e27a73fe68f92981e325911d52ad665ec470efea48a0091daa595a3a780fe13bd0e4e9bd1020000aa55"
	// {"devId":"bf0123","dps":{"1":false,"27":false}} after a zero return code.
	queryReplyHex = "000055aa000000020000000a0000003c00000000cf86245f80a43fd581e9f874f8bade1729a6794aa335cc1d4ac4a4d873d15ae1f46eea515a7e580b99a2f661d4c7c3493a8b7ff40000aa55"
	// {"devId":"bf0123","dps":{"1":true}} behind the version header, no return code.
	pushHex = "000055aa000000030000000800000047332e33000000000000000000000000cf86245f80a43fd581e9f874f8bade172125e1846bd4f824d1b2e468c239926631836d84bd28cb4b20619eb84882ca2af7a70ae20000aa55"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEncodeRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		seq  uint32
		cmd  uint32
		m    message
		want string
	}{
		{"control on", 1, cmdControl, message{DevID: testID, UID: testID, T: "1762963200", DPS: map[string]bool{"1": true}}, controlOnHex},
		{"query", 2, cmdQuery, message{GwID: testID, DevID: testID, UID: testID, T: "1762963200"}, queryHex},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f, err := encodeRequest(tt.seq, tt.cmd, tt.m, []byte(testKey))
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(f.encode()); got != tt.want {
				t.Errorf("frame\n got %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestReadFrame(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		in       string
		cmd      uint32
		code     uint32
		data     string
		frameErr string
	}{
		{"query reply with return code", queryReplyHex, cmdQuery, 0, `{"devId":"bf0123","dps":{"1":false,"27":false}}`, ""},
		{"push with version header", pushHex, cmdStatus, 0, `{"devId":"bf0123","dps":{"1":true}}`, ""},
		{"bare ack", "000055aa00000001000000070000000c00000000" + "%crc%" + "0000aa55", cmdControl, 0, "", ""},
		{"error in the clear", "000055aa00000001000000070000001d" + hex.EncodeToString([]byte("\x00\x00\x00\x01data format error")) + "%crc%0000aa55", cmdControl, 1, "data format error", ""},
		{"bad crc", strings.Replace(pushHex, "f7a70ae2", "f7a70ae3", 1), 0, 0, "", "CRC"},
		{"bad prefix", "000055ab" + pushHex[8:], 0, 0, "", "prefix"},
		{"bad suffix", pushHex[:len(pushHex)-8] + "0000aa56", 0, 0, "", "suffix"},
		{"huge length", "000055aa00000001000000070fffffff", 0, 0, "", "length"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := tt.in
			if i := strings.Index(in, "%crc%"); i >= 0 {
				in = in[:i] + hex.EncodeToString(crc(unhex(t, in[:i]))) + in[i+5:]
			}
			f, err := readFrame(bytes.NewReader(unhex(t, in)))
			if tt.frameErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.frameErr) {
					t.Fatalf("error %v, want one about %s", err, tt.frameErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			code, data, err := reply(f.payload, []byte(testKey))
			if err != nil || f.cmd != tt.cmd || code != tt.code || string(data) != tt.data {
				t.Errorf("got cmd %d code %d data %q err %v, want cmd %d code %d data %q", f.cmd, code, data, err, tt.cmd, tt.code, tt.data)
			}
		})
	}
}

func crc(b []byte) []byte {
	return binary.BigEndian.AppendUint32(nil, crc32.ChecksumIEEE(b))
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	for _, plain := range []string{"", "x", strings.Repeat("a", 16), `{"dps":{"1":true}}`} {
		enc, err := encrypt([]byte(plain), []byte(testKey))
		if err != nil {
			t.Fatal(err)
		}
		f := frame{seq: 9, cmd: cmdQuery, payload: enc}
		got, err := readFrame(bytes.NewReader(f.encode()))
		if err != nil {
			t.Fatal(err)
		}
		dec, err := decrypt(got.payload, []byte(testKey))
		if err != nil || string(dec) != plain || got.seq != 9 || got.cmd != cmdQuery {
			t.Errorf("%q came back as %q (seq %d cmd %d, %v)", plain, dec, got.seq, got.cmd, err)
		}
	}
	if _, err := decrypt(unhex(t, pushHex)[16+15:16+15+48], []byte("fedcba9876543210")); err == nil {
		t.Error("a wrong key decrypted")
	}
}

// fakePlug answers like the real one: a query with its state, a control with a bare ack followed by a push.
type fakePlug struct {
	t      *testing.T
	ln     net.Listener
	on     bool
	writes chan map[string]bool
	silent bool
}

func newFakePlug(t *testing.T, on, silent bool) *fakePlug {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &fakePlug{t: t, ln: ln, on: on, silent: silent, writes: make(chan map[string]bool, 4)}
	t.Cleanup(func() { ln.Close() })
	go p.serve()
	return p
}

func (p *fakePlug) serve() {
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.handle(conn)
	}
}

func (p *fakePlug) handle(conn net.Conn) {
	defer conn.Close()
	f, err := readFrame(conn)
	if err != nil || p.silent {
		time.Sleep(50 * time.Millisecond)
		return
	}
	payload := bytes.TrimPrefix(f.payload, versionHeader)
	plain, err := decrypt(payload, []byte(testKey))
	if err != nil {
		p.t.Errorf("plug could not decrypt: %v", err)
		return
	}
	var m message
	if err := json.Unmarshal(plain, &m); err != nil || m.DevID != testID || m.UID != testID || m.T == "" {
		p.t.Errorf("plug got %s (%v)", plain, err)
		return
	}
	state := func(seq, cmd uint32, header []byte) []byte {
		body, _ := json.Marshal(map[string]any{"devId": testID, "dps": map[string]bool{"1": p.on, "27": false}})
		enc, _ := encrypt(body, []byte(testKey))
		return frame{seq: seq, cmd: cmd, payload: append(header, enc...)}.encode()
	}
	switch f.cmd {
	case cmdQuery:
		_, _ = conn.Write(state(f.seq, cmdQuery, []byte{0, 0, 0, 0}))
	case cmdControl:
		p.writes <- m.DPS
		p.on = m.DPS["1"]
		_, _ = conn.Write(append(state(0, cmdStatus, bytes.Clone(versionHeader)), frame{seq: f.seq, cmd: cmdControl, payload: []byte{0, 0, 0, 0}}.encode()...))
	}
}

func TestPlug(t *testing.T) {
	t.Parallel()
	fake := newFakePlug(t, false, false)
	p := &Plug{ID: testID, Addr: fake.ln.Addr().String(), Key: testKey}
	ctx := context.Background()
	if on, err := p.Power(ctx); err != nil || on {
		t.Fatalf("Power = %v, %v; want off", on, err)
	}
	if err := p.SetPower(ctx, true); err != nil {
		t.Fatal(err)
	}
	if got := <-fake.writes; len(got) != 1 || !got["1"] {
		t.Errorf("wrote %v, want only power on", got)
	}
	if on, err := p.Power(ctx); err != nil || !on {
		t.Fatalf("Power = %v, %v; want on", on, err)
	}
}

func TestPlugUnreachable(t *testing.T) {
	t.Parallel()
	fake := newFakePlug(t, false, true)
	p := &Plug{ID: testID, Addr: fake.ln.Addr().String(), Key: testKey}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := p.Power(ctx); err == nil {
		t.Error("a silent plug answered")
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	if err := (&Plug{ID: testID, Addr: addr, Key: testKey}).SetPower(context.Background(), true); err == nil || !strings.Contains(err.Error(), "reach the plug") {
		t.Errorf("SetPower on a closed port = %v", err)
	}
}
