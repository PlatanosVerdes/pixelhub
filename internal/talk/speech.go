package talk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// maxText keeps a request within what the display can mouth.
const maxText = 400

// Speaker reads text aloud through a Wyoming text-to-speech server, such as wyoming-piper.
type Speaker struct {
	Addr string
	// Voice, when set, picks one of the server's voices instead of its default.
	Voice string
}

type wyomingEvent struct {
	Type          string          `json:"type"`
	Data          json.RawMessage `json:"data,omitempty"`
	DataLength    int             `json:"data_length,omitempty"`
	PayloadLength int             `json:"payload_length,omitempty"`
}

type audioFormat struct {
	Rate     int `json:"rate"`
	Width    int `json:"width"`
	Channels int `json:"channels"`
}

func (s Speaker) Say(ctx context.Context, text string) (PCM, error) {
	switch {
	case s.Addr == "":
		return PCM{}, errors.New("no speech server set up")
	case text == "":
		return PCM{}, errors.New("nothing to say")
	case len([]rune(text)) > maxText:
		return PCM{}, fmt.Errorf("text over %d characters", maxText)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return PCM{}, fmt.Errorf("speech server: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	req := map[string]any{"text": text}
	if s.Voice != "" {
		req["voice"] = map[string]string{"name": s.Voice}
	}
	data, _ := json.Marshal(req)
	line, _ := json.Marshal(wyomingEvent{Type: "synthesize", Data: data})
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return PCM{}, fmt.Errorf("speech server: %w", err)
	}
	return readSpeech(bufio.NewReader(conn))
}

// readSpeech collects the audio chunks up to audio-stop; Piper sends 16-bit mono.
func readSpeech(r *bufio.Reader) (PCM, error) {
	var format audioFormat
	var raw []byte
	for {
		head, err := r.ReadBytes('\n')
		if err != nil {
			return PCM{}, fmt.Errorf("speech server: %w", err)
		}
		var ev wyomingEvent
		if err := json.Unmarshal(head, &ev); err != nil {
			return PCM{}, fmt.Errorf("speech server sent %q: %w", head, err)
		}
		data := []byte(ev.Data)
		if ev.DataLength > 0 {
			if data, err = readN(r, ev.DataLength); err != nil {
				return PCM{}, err
			}
		}
		var payload []byte
		if ev.PayloadLength > 0 {
			if payload, err = readN(r, ev.PayloadLength); err != nil {
				return PCM{}, err
			}
		}
		switch ev.Type {
		case "audio-start", "audio-chunk":
			if len(data) > 0 {
				if err := json.Unmarshal(data, &format); err != nil {
					return PCM{}, fmt.Errorf("speech server audio format: %w", err)
				}
			}
			if format.Width != 2 || format.Channels != 1 {
				return PCM{}, fmt.Errorf("speech server sends %d-byte, %d-channel audio; want 16-bit mono", format.Width, format.Channels)
			}
			raw = append(raw, payload...)
			if time.Duration(len(raw)/2)*time.Second/time.Duration(max(1, format.Rate)) > MaxLength {
				return PCM{}, fmt.Errorf("speech runs over %s", MaxLength)
			}
		case "audio-stop":
			return fromLE(raw, format.Rate)
		case "error":
			return PCM{}, fmt.Errorf("speech server: %s", data)
		}
	}
}

func readN(r io.Reader, n int) ([]byte, error) {
	if n > 1<<20 {
		return nil, fmt.Errorf("speech server sent a %d-byte block", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, fmt.Errorf("speech server: %w", err)
	}
	return b, nil
}
