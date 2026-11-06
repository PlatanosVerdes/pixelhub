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
	formatPic  = 0x08
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
	About  string   `json:"Content"`
	Class  int      `json:"Classify"`
}

// CategoryOf names the category a picture was filed under, or "" when it has no tab of its own.
func CategoryOf(class int) string {
	for name, c := range Categories {
		if c == class && name != "new" && name != "recommended" && name != "top" {
			return name
		}
	}
	return ""
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
	return c.fileList(ctx, "GetCategoryFileListV2", "gallery list", map[string]any{
		"StartNum": start, "EndNum": start + perPage - 1, "Classify": category,
		"FileSize": size16, "FileType": typeAll, "FileSort": int(sort), "Version": 12, "RefreshIndex": 0,
	})
}

// Search returns one page of 16x16 pictures whose names or tags hold words, as the app's search
// finds them; it needs no account.
func (c *Client) Search(ctx context.Context, words string, page, perPage int) ([]Item, error) {
	start := (page-1)*perPage + 1
	return c.fileList(ctx, "SearchGalleryV2", "gallery search", map[string]any{
		"StartNum": start, "EndNum": start + perPage - 1, "Keywords": words,
		"FileSize": size16, "FileType": typeAll, "Version": 12,
	})
}

func (c *Client) fileList(ctx context.Context, command, what string, request map[string]any) ([]Item, error) {
	body, _ := json.Marshal(request)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.API+"/"+command, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", what, resp.StatusCode)
	}
	var out struct {
		ReturnCode    int    `json:"ReturnCode"`
		ReturnMessage string `json:"ReturnMessage"`
		FileList      []Item `json:"FileList"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxFile)).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if out.ReturnCode != 0 {
		return nil, fmt.Errorf("%s: code %d %s", what, out.ReturnCode, out.ReturnMessage)
	}
	return out.FileList, nil
}

// Download fetches a gallery file as stored, still encrypted.
func (c *Client) Download(ctx context.Context, fileID string) ([]byte, error) {
	if fileID == "" || strings.Contains(fileID, "..") {
		return nil, fmt.Errorf("%w %q", ErrBadFile, fileID)
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

var (
	ErrFormat  = errors.New("gallery: not a 16x16 animation file")
	ErrBadFile = errors.New("gallery: bad file id")
	// ErrRateLimited means the gallery wants fewer requests for a while.
	ErrRateLimited = errors.New("the Divoom gallery asks to slow down; try again in a few minutes")
)

// Encode builds a 16x16 gallery file from frames, the way the gallery stores them.
func Encode(frames []*image.RGBA, delay time.Duration) []byte {
	data := make([]byte, 0, len(frames)*frameBytes)
	for _, f := range frames {
		for y := range 16 {
			for x := range 16 {
				c := f.RGBAAt(x, y)
				data = append(data, c.R, c.G, c.B)
			}
		}
	}
	block, _ := aes.NewCipher(aesKey)
	cipher.NewCBCEncrypter(block, aesIV).CryptBlocks(data, data)
	ms := delay.Milliseconds()
	return append([]byte{formatAnim, 0, byte(ms >> 8), byte(ms)}, data...)
}

// Decode turns a 16x16 gallery file into its frames and the delay between them.
func Decode(file []byte) ([]*image.RGBA, time.Duration, error) {
	var delay time.Duration
	var data []byte
	switch {
	case len(file) > 1 && file[0] == formatPic:
		// Older uploads: one still picture, the cipher text right after the format byte.
		data = append(data, file[1:]...)
	case len(file) >= 4 && file[0] == formatAnim:
		// Header: format, one unused byte, the frame delay in ms (big endian); the cipher text follows.
		delay = time.Duration(int(file[2])<<8|int(file[3])) * time.Millisecond
		data = append(data, file[4:]...)
	default:
		return nil, 0, ErrFormat
	}
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
