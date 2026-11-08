package bridge

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

const (
	greetingHex = "01090004bd5513011e00510102"
	ackHex      = "01060004325500910002"
)

func TestParser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		reads []string
		want  []string
	}{
		{"one frame", []string{greetingHex}, []string{greetingHex}},
		{"split across reads", []string{greetingHex[:5*2], greetingHex[5*2 : 9*2], greetingHex[9*2:]}, []string{greetingHex}},
		{"header split", []string{"01", "09", greetingHex[4:]}, []string{greetingHex}},
		{"several per read", []string{ackHex + greetingHex + ackHex}, []string{ackHex, greetingHex, ackHex}},
		{"lone link down byte", []string{"96"}, []string{"lost"}},
		{"link down between frames", []string{ackHex + "96", greetingHex}, []string{ackHex, "lost", greetingHex}},
		{"noise before a frame", []string{"ff00" + ackHex}, []string{ackHex}},
		{"bad end byte resyncs", []string{"0106000432550091000301" + ackHex[2:]}, []string{ackHex}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var p parser
			var got []string
			for _, r := range tt.reads {
				b, err := hex.DecodeString(r)
				if err != nil {
					t.Fatal(err)
				}
				for _, ev := range p.feed(b) {
					got = append(got, describe(ev))
				}
			}
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func describe(ev event) string {
	switch {
	case ev.lost:
		return "lost"
	case ev.err != nil:
		return fmt.Sprint("error ", ev.err)
	default:
		return hex.EncodeToString(ev.frame)
	}
}

func TestParserBattery(t *testing.T) {
	t.Parallel()
	var p parser
	got := p.feed([]byte{0x42})
	got = append(got, p.feed([]byte{0x07, 0x42, 0x0c, 0x96})...)
	if len(got) != 2 || got[0].battery != 8 || !got[1].lost {
		t.Errorf("a battery level split over reads, a bad level skipped, then link down: %+v", got)
	}
}

func TestParserSpeaker(t *testing.T) {
	t.Parallel()
	var p parser
	got := p.feed([]byte{0x41})
	got = append(got, p.feed([]byte{0x00, 0x41, 0x01})...)
	if len(got) != 2 || got[0].speaker != 1 || got[1].speaker != 2 {
		t.Errorf("no speaker split over reads, then a talk heard: %+v", got)
	}
}

func TestParserSensors(t *testing.T) {
	t.Parallel()
	var p parser
	got := p.feed([]byte{0x4c, 0x00, 0x53, 0x01, 0x4c, 0xff})
	if len(got) != 3 || got[0].light != 1 || !got[1].sound || got[2].light != 256 {
		t.Errorf("light 0, a sound, light 255: %+v", got)
	}
}
