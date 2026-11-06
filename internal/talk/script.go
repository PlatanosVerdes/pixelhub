package talk

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Line is a piece of text said in one mood, and the pause after it.
type Line struct {
	Text      string
	Mood      Mood
	Pause     time.Duration
	PauseMood Mood
}

// Span is a mood from one time to another into a clip.
type Span struct {
	Mood     Mood
	From, To time.Duration
}

var (
	sentenceEnd = regexp.MustCompile(`\.{3,}|…|[.!?]+`)
	laugh       = regexp.MustCompile(`(?i)\b(?:j[aeiou]){2,}\b`)
)

// Script splits text where its sentences end; the ending sets the mood and the pause:
// "!" excited, "?" asking, "..." a long thinking pause, and a "jaja" laughs.
func Script(text string) []Line {
	var lines []Line
	add := func(chunk, end string) {
		line := Line{Text: strings.TrimSpace(chunk)}
		switch {
		case strings.HasPrefix(end, "...") || end == "…":
			line.Pause, line.PauseMood = 900*time.Millisecond, Thinking
		case strings.Contains(end, "!"):
			line.Mood, line.Pause, line.PauseMood = Excited, 350*time.Millisecond, Excited
		case strings.Contains(end, "?"):
			line.Mood, line.Pause, line.PauseMood = Asking, 450*time.Millisecond, Asking
		case end != "":
			line.Pause = 300 * time.Millisecond
		}
		if laugh.MatchString(chunk) {
			line.Mood = Laughing
		}
		if !strings.ContainsFunc(line.Text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
			if n := len(lines); n > 0 && line.Pause > 0 {
				lines[n-1].Pause += line.Pause
			}
			return
		}
		lines = append(lines, line)
	}
	start := 0
	for _, m := range sentenceEnd.FindAllStringIndex(text, -1) {
		add(text[start:m[1]], text[m[0]:m[1]])
		start = m[1]
	}
	add(text[start:], "")
	if n := len(lines); n > 0 {
		lines[n-1].Pause = 0
	}
	return lines
}

// Speak reads each line with v and leaves its pause after it, saying where each mood falls.
func Speak(ctx context.Context, v interface {
	Say(context.Context, string) (PCM, error)
}, lines []Line) (PCM, []Span, error) {
	if len(lines) == 0 {
		return PCM{}, nil, errors.New("nothing to say")
	}
	var out PCM
	var spans []Span
	at := func() time.Duration { return out.Duration() }
	for _, l := range lines {
		pcm, err := v.Say(ctx, l.Text)
		if err != nil {
			return PCM{}, nil, err
		}
		if out.Rate == 0 {
			out.Rate = pcm.Rate
		}
		if pcm.Rate != out.Rate {
			return PCM{}, nil, fmt.Errorf("speech came at %d Hz, then %d Hz", out.Rate, pcm.Rate)
		}
		from := at()
		out.Samples = append(out.Samples, pcm.Samples...)
		spans = append(spans, Span{l.Mood, from, at()})
		if l.Pause > 0 {
			from = at()
			out = out.withTrailingSilence(l.Pause)
			spans = append(spans, Span{l.PauseMood, from, at()})
		}
		if at() > MaxLength {
			return PCM{}, nil, fmt.Errorf("speech runs over %s", MaxLength)
		}
	}
	return out, spans, nil
}

// Stretch moves spans timed on audio that lasted from onto audio that lasts to, as a voice effect changes its speed.
func Stretch(spans []Span, from, to time.Duration) []Span {
	if from <= 0 || from == to {
		return spans
	}
	scale := func(d time.Duration) time.Duration { return time.Duration(float64(d) * float64(to) / float64(from)) }
	out := make([]Span, len(spans))
	for i, s := range spans {
		out[i] = Span{s.Mood, scale(s.From), scale(s.To)}
	}
	return out
}
