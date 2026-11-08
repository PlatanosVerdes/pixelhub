package talk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"math"
	"net"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

type part struct {
	dur  time.Duration
	loud float64
}

// speech is a 220 Hz tone at each part's loudness (0 is silence), at Rate.
func speech(parts ...part) PCM {
	var s []int16
	for _, p := range parts {
		n := int(p.dur.Seconds() * Rate)
		for i := range n {
			s = append(s, int16(p.loud*math.Sin(2*math.Pi*220*float64(i)/Rate)))
		}
	}
	return PCM{Rate: Rate, Samples: s}
}

func shapeOf(t *testing.T, img image.Image) Shape {
	t.Helper()
	for _, s := range []Shape{Shut, Small, Half, Wide, Blink} {
		if same(img, Face(s, Calm)) {
			return s
		}
	}
	t.Fatal("a frame that is none of Mosko's faces")
	return 0
}

func same(a, b image.Image) bool {
	for y := range 16 {
		for x := range 16 {
			if a.At(x, y) != b.At(x, y) {
				return false
			}
		}
	}
	return true
}

// at is the shape on screen at d into the voice, after the silence that wakes the speaker.
func at(t *testing.T, frames []display.Frame, d time.Duration) Shape {
	t.Helper()
	d += wake
	var end time.Duration
	for _, f := range frames {
		end += f.Delay
		if d < end {
			return shapeOf(t, f.Image)
		}
	}
	t.Fatalf("%s is past the clip", d)
	return 0
}

func TestMouthFollowsTheSilences(t *testing.T) {
	t.Parallel()
	p := speech(part{500 * time.Millisecond, 0}, part{400 * time.Millisecond, 20000}, part{300 * time.Millisecond, 0},
		part{300 * time.Millisecond, 4000}, part{2 * time.Second, 0}, part{200 * time.Millisecond, 20000})
	c, _ := Clip(p, nil, 0)
	if got := Length(c); got != (p.Duration() + wake + restFor).Round(time.Millisecond) {
		t.Errorf("the mouth lasts %s, the sound %s", got, p.Duration())
	}
	for _, tc := range []struct {
		at   time.Duration
		want Shape
	}{
		{100 * time.Millisecond, Shut}, {450 * time.Millisecond, Shut},
		{600 * time.Millisecond, Wide}, {850 * time.Millisecond, Wide},
		{1000 * time.Millisecond, Shut}, {1150 * time.Millisecond, Shut},
		{1350 * time.Millisecond, Small},
		{1600 * time.Millisecond, Shut}, {2070 * time.Millisecond, Blink}, {3400 * time.Millisecond, Shut},
		{3600 * time.Millisecond, Wide},
	} {
		if got := at(t, c.Frames, tc.at); got != tc.want {
			t.Errorf("at %s the mouth is %d, want %d", tc.at, got, tc.want)
		}
	}
	if last := c.Frames[len(c.Frames)-1]; shapeOf(t, last.Image) != Shut {
		t.Errorf("the clip ends on %d, want the mouth shut", shapeOf(t, last.Image))
	}
	if len(c.Sound) != 44+2*(len(p.Samples)+int(wake.Seconds()*Rate)) || c.SoundAfter != 0 {
		t.Errorf("sound of %d bytes after %s", len(c.Sound), c.SoundAfter)
	}
}

func TestLagShiftsTheMouthOrTheSound(t *testing.T) {
	t.Parallel()
	p := speech(part{300 * time.Millisecond, 20000}, part{300 * time.Millisecond, 0})
	late, _ := Clip(p, nil, 250*time.Millisecond)
	if late.FramesAfter != 250*time.Millisecond || late.SoundAfter != 0 {
		t.Errorf("a speaker behind the display should hold the faces back: faces after %s, sound after %s", late.FramesAfter, late.SoundAfter)
	}
	early, _ := Clip(p, nil, -250*time.Millisecond)
	if early.SoundAfter != 250*time.Millisecond || early.FramesAfter != 0 {
		t.Errorf("a speaker ahead of the display should wait: faces after %s, sound after %s", early.FramesAfter, early.SoundAfter)
	}
	if got, want := Length(early), p.Duration()+wake+restFor+250*time.Millisecond; got != want {
		t.Errorf("talks for %s, want %s", got, want)
	}
}

func TestLongClipsKeepEverySyllable(t *testing.T) {
	t.Parallel()
	var parts []part
	for range 100 {
		parts = append(parts, part{180 * time.Millisecond, 20000}, part{120 * time.Millisecond, 0})
	}
	c, _ := Clip(speech(parts...), nil, 0)
	for i := range 100 {
		start := time.Duration(i) * 300 * time.Millisecond
		if got := at(t, c.Frames, start+90*time.Millisecond); got == Shut {
			t.Errorf("syllable %d shows the mouth shut", i)
		}
		if got := at(t, c.Frames, start+240*time.Millisecond); got != Shut {
			t.Errorf("the gap after syllable %d shows %d", i, got)
		}
	}
}

func TestQuietClipKeepsTheMouthShut(t *testing.T) {
	t.Parallel()
	c, _ := Clip(speech(part{time.Second, 50}), nil, 0)
	for _, f := range c.Frames {
		if s := shapeOf(t, f.Image); s != Shut && s != Blink {
			t.Errorf("the mouth opens to %d in a near-silent clip", s)
		}
	}
}

func TestSpeakerReadsWyoming(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan map[string]any, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadBytes('\n')
		var ev struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		_ = json.Unmarshal(line, &ev)
		got <- ev.Data
		format := `{"rate":22050,"width":2,"channels":1}`
		fmt.Fprintf(c, "{\"type\":\"audio-start\",\"data_length\":%d}\n%s", len(format), format)
		fmt.Fprintf(c, "{\"type\":\"audio-chunk\",\"data\":%s,\"payload_length\":4}\n\x01\x00\x02\x00", format)
		fmt.Fprintf(c, "{\"type\":\"audio-chunk\",\"data_length\":%d,\"payload_length\":2}\n%s\xff\xff", len(format), format)
		fmt.Fprint(c, "{\"type\":\"audio-stop\"}\n")
	}()
	pcm, err := Speaker{Addr: ln.Addr().String(), Voice: "es_ES-davefx-medium"}.Say(context.Background(), "hola")
	if err != nil {
		t.Fatal(err)
	}
	if pcm.Rate != 22050 || fmt.Sprint(pcm.Samples) != "[1 2 -1]" {
		t.Errorf("got %d Hz %v", pcm.Rate, pcm.Samples)
	}
	req := <-got
	if req["text"] != "hola" || req["voice"].(map[string]any)["name"] != "es_ES-davefx-medium" {
		t.Errorf("asked for %v", req)
	}
}

func TestDecodeKeepsTheLengthAtAnyPitch(t *testing.T) {
	t.Parallel()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	in := speech(part{time.Second, 20000})
	in.Rate = 44100
	in.Samples = append(in.Samples, in.Samples...)
	for _, pitch := range []float64{1, 1.4, 0.7, 2} {
		for name, fx := range Effects {
			out, err := Decoder{FFmpeg: ffmpeg}.Decode(context.Background(), in.WAV(), pitch, name)
			if err != nil {
				t.Fatalf("pitch %.1f %q: %v", pitch, name, err)
			}
			want := time.Duration(float64(in.Duration()) / fx.Tempo)
			if out.Rate != Rate || (out.Duration()-want).Abs() > 100*time.Millisecond {
				t.Errorf("pitch %.1f %q: %d Hz for %s, want %d Hz for %s", pitch, name, out.Rate, out.Duration(), Rate, want)
			}
		}
	}
	if _, err := (Decoder{FFmpeg: ffmpeg}).Decode(context.Background(), []byte("not audio"), 1, ""); err == nil {
		t.Error("garbage decoded")
	}
	if _, err := (Decoder{FFmpeg: ffmpeg}).Decode(context.Background(), in.WAV(), 1, "opera"); err == nil {
		t.Error("an unknown effect was taken")
	}
}

func TestLongClipsKeepTheirPauses(t *testing.T) {
	t.Parallel()
	var parts []part
	for range 25 {
		for range 6 {
			parts = append(parts, part{90 * time.Millisecond, 20000}, part{40 * time.Millisecond, 3000})
		}
		parts = append(parts, part{220 * time.Millisecond, 0})
	}
	c, _ := Clip(speech(parts...), nil, 0)
	word := 6 * 130 * time.Millisecond
	for i := range 25 {
		start := time.Duration(i) * (word + 220*time.Millisecond)
		if got := at(t, c.Frames, start+word+110*time.Millisecond); got != Shut {
			t.Errorf("pause %d shows %d, want the mouth shut", i, got)
		}
		if got := at(t, c.Frames, start+word/2); got == Shut {
			t.Errorf("word %d shows the mouth shut", i)
		}
	}
}

func TestScript(t *testing.T) {
	t.Parallel()
	got := Script("Hola, soy Mosko. Vivo aquí... ¡Qué bien! ¿Y tú...? Jajaja")
	want := []Line{
		{"Hola, soy Mosko.", Calm, 300 * time.Millisecond, Calm},
		{"Vivo aquí...", Calm, 900 * time.Millisecond, Thinking},
		{"¡Qué bien!", Excited, 350 * time.Millisecond, Excited},
		{"¿Y tú...", Calm, 1350 * time.Millisecond, Thinking},
		{"Jajaja", Laughing, 0, Calm},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	if got := Script(" ... "); len(got) != 0 {
		t.Errorf("punctuation alone said %v", got)
	}
}

// toneVoice says each text as a loud tone, 50 ms a character.
type toneVoice struct{}

func (toneVoice) Say(_ context.Context, text string) (PCM, error) {
	return speech(part{time.Duration(len([]rune(text))) * 50 * time.Millisecond, 20000}), nil
}

func eyesOf(t *testing.T, img image.Image) Mood {
	t.Helper()
	for _, m := range []Mood{Calm, Excited, Asking, Thinking, Laughing} {
		face, ok := Face(Shut, m), true
		for y := eyeRow; y < mouthRow && ok; y++ {
			for x := range 16 {
				if img.At(x, y) != face.At(x, y) {
					ok = false
					break
				}
			}
		}
		if ok {
			return m
		}
	}
	return -1
}

func TestMoodsFallWhereTheyAreSaid(t *testing.T) {
	t.Parallel()
	p, spans, err := Speak(context.Background(), toneVoice{}, Script("¡Hola tú! Pues... ¿qué?"))
	if err != nil {
		t.Fatal(err)
	}
	// "¡Hola tú!" 450 ms, 350 ms pause, "Pues..." 350 ms, 900 ms pause, "¿qué?" 250 ms.
	if (p.Duration() - 2300*time.Millisecond).Abs() > time.Millisecond {
		t.Fatalf("said for %s", p.Duration())
	}
	c, _ := Clip(p, spans, 0)
	mood := func(d time.Duration) Mood {
		var end time.Duration
		for _, f := range c.Frames {
			end += f.Delay
			if d+wake < end {
				return eyesOf(t, f.Image)
			}
		}
		t.Fatalf("%s is past the clip", d)
		return 0
	}
	for _, tc := range []struct {
		at   time.Duration
		want Mood
	}{
		{200 * time.Millisecond, Excited}, {600 * time.Millisecond, Excited},
		{950 * time.Millisecond, Calm}, {1500 * time.Millisecond, Thinking},
		{2150 * time.Millisecond, Asking},
	} {
		if got := mood(tc.at); got != tc.want {
			t.Errorf("at %s the eyes are %d, want %d", tc.at, got, tc.want)
		}
	}
	last := c.Frames[len(c.Frames)-1]
	if eyesOf(t, last.Image) != Calm || shapeOf(t, last.Image) != Shut || last.Delay != restFor {
		t.Errorf("the clip ends on eyes %d for %s, want the calm face for %s", eyesOf(t, last.Image), last.Delay, restFor)
	}
	if got := mood(-wake / 2); got != Calm {
		t.Errorf("before the voice the eyes are %d, want calm", got)
	}
	stretched := Stretch(spans, p.Duration(), p.Duration()/2)
	if (stretched[1].From - spans[1].From/2).Abs() > time.Microsecond {
		t.Errorf("a faster voice moved the pause to %s, want %s", stretched[1].From, spans[1].From/2)
	}
}

func TestNoises(t *testing.T) {
	t.Parallel()
	for _, kind := range NoiseKinds {
		for range 20 {
			n, err := MakeNoise(kind)
			if err != nil {
				t.Fatal(err)
			}
			if d := n.PCM.Duration(); d < 100*time.Millisecond || d > 5*time.Second {
				t.Errorf("%s lasts %s", kind, d)
			}
			var peak int16
			for _, v := range n.PCM.Samples {
				peak = max(peak, v, -v)
			}
			if peak < 20000 {
				t.Errorf("%s peaks at %d, want it loud", kind, peak)
			}
			_, poses := n.Clip(0)
			open := slices.ContainsFunc(poses, func(p Pose) bool { return p.Shape != Shut && p.Shape != Blink })
			if open != (kind != "fart") {
				t.Errorf("%s opens the mouth: %v", kind, open)
			}
			if kind == "fart" && !slices.ContainsFunc(poses, func(p Pose) bool { return p.Mood == Laughing }) {
				t.Errorf("a fart is not laughed at: %v", poses)
			}
		}
	}
	if _, err := MakeNoise("sneeze"); !errors.Is(err, ErrNoNoise) {
		t.Errorf("an unknown noise: %v", err)
	}
}
