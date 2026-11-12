package hfp

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type unit struct {
	in  *strings.Reader
	out bytes.Buffer
}

func (u *unit) Read(p []byte) (int, error)  { return u.in.Read(p) }
func (u *unit) Write(p []byte) (int, error) { return u.out.Write(p) }

// The handshake the Evo made with an iPhone, captured on 2026-11-05.
const evoHandshake = "AT+BRSF=191\rAT+BAC=1,2\rAT+CIND=?\rAT+CIND?\rAT+CMER=3, 0, 0, 1\rAT+CHLD=?\rAT+CCWA=1\rAT+CLIP=1\r" +
	"AT+NREC=0\rAT+CGMI\rAT+CGMM\rAT+CCLK?\rAT+XAPL=ABCD-1234-0100,1\rAT+IPHONEACCEV=1,1,4\r"

func TestServeAnswersLikeAnIPhone(t *testing.T) {
	t.Parallel()
	u := &unit{in: strings.NewReader(evoHandshake)}
	var levels []int
	now := func() time.Time { return time.Date(2026, 11, 12, 16, 0, 36, 0, time.UTC) }
	if err := Serve(u, now, func(l int) { levels = append(levels, l) }); !errors.Is(err, io.EOF) {
		t.Fatalf("Serve = %v, want EOF", err)
	}
	if len(levels) != 1 || levels[0] != 4 {
		t.Errorf("levels = %v, want [4]", levels)
	}
	out := u.out.String()
	for _, want := range []string{"\r\n+BRSF:1007\r\n", "\r\n+XAPL=iPhone,7\r\n", "\r\n+CCLK: \"26/11/12, 16:00:36\"\r\n", "\r\n" + indicatorValues + "\r\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in the replies", want)
		}
	}
	if got := strings.Count(out, "\r\nOK\r\n"); got != 14 {
		t.Errorf("%d OKs, want one per command (14)", got)
	}
}

func TestBatteryLevel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cmd  string
		want int
		ok   bool
	}{
		{"AT+IPHONEACCEV=1,1,8", 8, true},
		{"AT+IPHONEACCEV=2,2,0,1,3", 3, true},
		{"AT+IPHONEACCEV=1,2,1", 0, false},
		{"AT+IPHONEACCEV=1,1,12", 0, false},
		{"AT+IPHONEACCEV=1,1", 0, false},
		{"AT+CIND?", 0, false},
	} {
		got, ok := batteryLevel(tc.cmd)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s = %d %v, want %d %v", tc.cmd, got, ok, tc.want, tc.ok)
		}
	}
}
