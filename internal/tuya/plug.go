package tuya

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"
)

// Port is where a Tuya device listens for local control.
const Port = "6668"

const timeout = 5 * time.Second

// powerDP is the plug's on/off switch. Only this one is ever written: 27 is the light ring.
const powerDP = "1"

// Plug is one device; each call opens its own short connection, since the plug drops local
// connections while the phone app is open.
type Plug struct {
	ID   string
	Addr string
	// Key is the 16-character local key, used as the AES-128 key.
	Key string
	seq atomic.Uint32
}

type message struct {
	GwID  string          `json:"gwId,omitempty"`
	DevID string          `json:"devId"`
	UID   string          `json:"uid"`
	T     string          `json:"t"`
	DPS   map[string]bool `json:"dps,omitempty"`
}

// Power reports whether the plug is switched on.
func (p *Plug) Power(ctx context.Context) (bool, error) {
	f, err := p.request(cmdQuery, message{GwID: p.ID})
	if err != nil {
		return false, err
	}
	var on bool
	err = p.exchange(ctx, f, func(cmd uint32, data []byte) (bool, error) {
		if (cmd != cmdQuery && cmd != cmdStatus) || data == nil {
			return false, nil
		}
		var got struct{ DPS map[string]any }
		if err := json.Unmarshal(data, &got); err != nil {
			return false, fmt.Errorf("plug status %q: %w", data, err)
		}
		v, ok := got.DPS[powerDP].(bool)
		if !ok {
			return false, fmt.Errorf("plug status %q has no power switch", data)
		}
		on = v
		return true, nil
	})
	return on, err
}

// SetPower switches the plug on or off.
func (p *Plug) SetPower(ctx context.Context, on bool) error {
	f, err := p.request(cmdControl, message{DPS: map[string]bool{powerDP: on}})
	if err != nil {
		return err
	}
	return p.exchange(ctx, f, func(cmd uint32, _ []byte) (bool, error) { return cmd == cmdControl, nil })
}

func (p *Plug) request(cmd uint32, m message) (frame, error) {
	m.DevID, m.UID, m.T = p.ID, p.ID, strconv.FormatInt(time.Now().Unix(), 10)
	return encodeRequest(p.seq.Add(1), cmd, m, []byte(p.Key))
}

func encodeRequest(seq, cmd uint32, m message, key []byte) (frame, error) {
	plain, err := json.Marshal(m)
	if err != nil {
		return frame{}, err
	}
	payload, err := encrypt(plain, key)
	if err != nil {
		return frame{}, err
	}
	if cmd == cmdControl {
		payload = append(bytes.Clone(versionHeader), payload...)
	}
	return frame{seq: seq, cmd: cmd, payload: payload}, nil
}

// exchange sends f and reads replies until done takes one; a non-zero return code is an error.
func (p *Plug) exchange(ctx context.Context, f frame, done func(cmd uint32, data []byte) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", p.Addr)
	if err != nil {
		return fmt.Errorf("reach the plug: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	if _, err := conn.Write(f.encode()); err != nil {
		return fmt.Errorf("send to the plug: %w", err)
	}
	for {
		r, err := readFrame(conn)
		if err != nil {
			return fmt.Errorf("read the plug's reply: %w", err)
		}
		code, data, err := reply(r.payload, []byte(p.Key))
		if err != nil {
			return fmt.Errorf("plug reply: %w", err)
		}
		if code != 0 {
			return fmt.Errorf("plug answered %d: %q", code, data)
		}
		ok, err := done(r.cmd, data)
		if ok || err != nil {
			return err
		}
	}
}
