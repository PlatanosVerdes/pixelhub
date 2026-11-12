// Package tuya switches a Tuya smart plug over its local protocol, version 3.3.
package tuya

import (
	"bytes"
	"crypto/aes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

const (
	cmdControl = 7
	cmdStatus  = 8
	cmdQuery   = 10

	prefix  = 0x000055AA
	suffix  = 0x0000AA55
	version = "3.3"
	// headerLen is the prefix, sequence, command and length fields; trailerLen the CRC and suffix.
	headerLen, trailerLen = 16, 8
	maxPayload            = 4096
)

// versionHeader is "3.3" and 12 zero bytes, in front of a CONTROL payload and some replies.
var versionHeader = append([]byte(version), make([]byte, 12)...)

type frame struct {
	seq, cmd uint32
	payload  []byte
}

func (f frame) encode() []byte {
	b := binary.BigEndian.AppendUint32(nil, prefix)
	b = binary.BigEndian.AppendUint32(b, f.seq)
	b = binary.BigEndian.AppendUint32(b, f.cmd)
	b = binary.BigEndian.AppendUint32(b, uint32(len(f.payload)+trailerLen))
	b = append(b, f.payload...)
	b = binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
	return binary.BigEndian.AppendUint32(b, suffix)
}

func readFrame(r io.Reader) (frame, error) {
	head := make([]byte, headerLen)
	if _, err := io.ReadFull(r, head); err != nil {
		return frame{}, err
	}
	if binary.BigEndian.Uint32(head) != prefix {
		return frame{}, fmt.Errorf("frame prefix %x", head[:4])
	}
	n := binary.BigEndian.Uint32(head[12:])
	if n < trailerLen || n > maxPayload+trailerLen {
		return frame{}, fmt.Errorf("frame length %d", n)
	}
	b := append(head, make([]byte, n)...)
	if _, err := io.ReadFull(r, b[headerLen:]); err != nil {
		return frame{}, err
	}
	body, trailer := b[:len(b)-trailerLen], b[len(b)-trailerLen:]
	if got, want := binary.BigEndian.Uint32(trailer), crc32.ChecksumIEEE(body); got != want {
		return frame{}, fmt.Errorf("frame CRC %08x, want %08x", got, want)
	}
	if binary.BigEndian.Uint32(trailer[4:]) != suffix {
		return frame{}, fmt.Errorf("frame suffix %x", trailer[4:])
	}
	return frame{seq: binary.BigEndian.Uint32(head[4:]), cmd: binary.BigEndian.Uint32(head[8:]), payload: body[headerLen:]}, nil
}

// reply splits a device's payload into its return code and its decrypted data, nil for a bare ack.
func reply(payload []byte, key []byte) (code uint32, data []byte, err error) {
	if !bytes.HasPrefix(payload, []byte(version)) {
		if len(payload) < 4 {
			return 0, nil, fmt.Errorf("reply of %d bytes", len(payload))
		}
		code, payload = binary.BigEndian.Uint32(payload), payload[4:]
	}
	payload = bytes.TrimPrefix(payload, versionHeader)
	if len(payload) == 0 {
		return code, nil, nil
	}
	// Errors such as "data format error" come back in the clear.
	if len(payload)%aes.BlockSize != 0 {
		return code, payload, nil
	}
	data, err = decrypt(payload, key)
	return code, data, err
}

func encrypt(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	out := append(bytes.Clone(plain), bytes.Repeat([]byte{byte(pad)}, pad)...)
	for i := 0; i < len(out); i += aes.BlockSize {
		block.Encrypt(out[i:], out[i:])
	}
	return out, nil
}

func decrypt(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := bytes.Clone(data)
	for i := 0; i < len(out); i += aes.BlockSize {
		block.Decrypt(out[i:], out[i:])
	}
	pad := int(out[len(out)-1])
	if pad == 0 || pad > aes.BlockSize || !bytes.Equal(out[len(out)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		return nil, errors.New("bad padding: wrong local key?")
	}
	return out[:len(out)-pad], nil
}
