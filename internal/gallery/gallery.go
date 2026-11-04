// Package gallery reads the Divoom app's community gallery: listing needs no account, and the
// 16x16 files decode with a key that is the same for every client.
package gallery

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultAPI   = "https://app.divoom-gz.com"
	DefaultFiles = "https://f.divoom-gz.com"

	size16     = 1
	typeAll    = 5
	frameBytes = 16 * 16 * 3
	maxFile    = 1 << 20
	formatAnim = 0x09
)

var (
	aesKey = []byte("78hrey23y28ogs89")
	aesIV  = []byte("1234567890123456")
)

type Sort int

const (
	Newest    Sort = 0
	MostLiked Sort = 1
)

// Categories are the app's own numbers; only the ones that still answer are listed.
var Categories = map[string]int{
	"new": 0, "default": 1, "character": 3, "emoji": 4, "daily": 5, "nature": 6, "symbol": 7,
	"pattern": 8, "creative": 9, "top": 14, "festival": 17, "recommended": 18, "plant": 31,
	"animal": 32, "person": 33, "food": 35,
}

type Item struct {
	ID     int64    `json:"GalleryId"`
	Name   string   `json:"FileName"`
	Author string   `json:"UserName"`
	Tags   []string `json:"FileTagArray"`
	Likes  int      `json:"LikeCnt"`
	FileID string   `json:"FileId"`
}

type Client struct {
	HTTP  *http.Client
	API   string
	Files string
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 20 * time.Second}, API: DefaultAPI, Files: DefaultFiles}
}

// List returns one page of 16x16 pictures and animations of a category, page counted from 1.
func (c *Client) List(ctx context.Context, category int, sort Sort, page, perPage int) ([]Item, error) {
	start := (page-1)*perPage + 1
	body, _ := json.Marshal(map[string]int{
		"StartNum": start, "EndNum": start + perPage - 1, "Classify": category,
		"FileSize": size16, "FileType": typeAll, "FileSort": int(sort), "Version": 12, "RefreshIndex": 0,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.API+"/GetCategoryFileListV2", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gallery list: HTTP %d", resp.StatusCode)
	}
	var out struct {
		ReturnCode    int    `json:"ReturnCode"`
		ReturnMessage string `json:"ReturnMessage"`
		FileList      []Item `json:"FileList"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxFile)).Decode(&out); err != nil {
		return nil, fmt.Errorf("gallery list: %w", err)
	}
	if out.ReturnCode != 0 {
		return nil, fmt.Errorf("gallery list: code %d %s", out.ReturnCode, out.ReturnMessage)
	}
	return out.FileList, nil
}

// Download fetches a gallery file as stored, still encrypted.
func (c *Client) Download(ctx context.Context, fileID string) ([]byte, error) {
	if fileID == "" || strings.Contains(fileID, "..") {
		return nil, fmt.Errorf("gallery download: bad file id %q", fileID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Files+"/"+strings.TrimPrefix(fileID, "/"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gallery download: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxFile))
}

var ErrFormat = errors.New("gallery: not a 16x16 animation file")

// Decode turns a 16x16 gallery file into its frames and the delay between them.
func Decode(file []byte) ([]*image.RGBA, time.Duration, error) {
	if len(file) < 4 || file[0] != formatAnim {
		return nil, 0, ErrFormat
	}
	// Header: format, one unused byte, the frame delay in ms (big endian); the cipher text follows.
	delay := time.Duration(int(file[2])<<8|int(file[3])) * time.Millisecond
	data := append([]byte{}, file[4:]...)
	if len(data)%aes.BlockSize != 0 || len(data) < frameBytes {
		return nil, 0, ErrFormat
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, 0, err
	}
	cipher.NewCBCDecrypter(block, aesIV).CryptBlocks(data, data)

	frames := make([]*image.RGBA, 0, len(data)/frameBytes)
	for off := 0; off+frameBytes <= len(data); off += frameBytes {
		img := image.NewRGBA(image.Rect(0, 0, 16, 16))
		for i := range 256 {
			p := data[off+i*3:]
			img.SetRGBA(i%16, i/16, color.RGBA{p[0], p[1], p[2], 255})
		}
		frames = append(frames, img)
	}
	return frames, delay, nil
}

// GIF encodes decoded frames so they can sit in an ambient folder like any uploaded picture.
func GIF(frames []*image.RGBA, delay time.Duration) ([]byte, error) {
	if len(frames) == 0 {
		return nil, ErrFormat
	}
	centis := max(2, int(delay/(10*time.Millisecond)))
	anim := &gif.GIF{}
	for _, f := range frames {
		p := image.NewPaletted(f.Bounds(), paletteOf(f))
		draw.Draw(p, p.Rect, f, image.Point{}, draw.Src)
		anim.Image = append(anim.Image, p)
		anim.Delay = append(anim.Delay, centis)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, anim); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// paletteOf keeps a frame's exact colours when it has at most 256, which a 16x16 frame nearly always does.
func paletteOf(img *image.RGBA) color.Palette {
	seen := map[color.RGBA]bool{}
	var pal color.Palette
	for i := 0; i < len(img.Pix); i += 4 {
		c := color.RGBA{img.Pix[i], img.Pix[i+1], img.Pix[i+2], 255}
		if !seen[c] {
			seen[c] = true
			pal = append(pal, c)
			if len(pal) > 256 {
				return palette.Plan9
			}
		}
	}
	return pal
}
