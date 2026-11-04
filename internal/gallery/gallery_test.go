package gallery

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func solid(c color.RGBA) []byte {
	out := make([]byte, frameBytes)
	for i := 0; i < frameBytes; i += 3 {
		out[i], out[i+1], out[i+2] = c.R, c.G, c.B
	}
	return out
}

func TestDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	first := solid(color.RGBA{10, 20, 30, 255})
	first[0], first[1], first[2] = 255, 0, 0
	data := bytes.Join([][]byte{first, solid(color.RGBA{0, 0, 0, 255})}, nil)
	block, _ := aes.NewCipher(aesKey)
	cipher.NewCBCEncrypter(block, aesIV).CryptBlocks(data, data)

	file := append([]byte{formatAnim, 0, 0, 120}, data...)

	got, delay, err := Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	if delay != 120*time.Millisecond {
		t.Errorf("delay %s, want 120ms", delay)
	}
	if len(got) != 2 {
		t.Fatalf("%d frames, want 2", len(got))
	}
	if c := got[0].RGBAAt(0, 0); c != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("first pixel %v", c)
	}
	if c := got[0].RGBAAt(15, 15); c != (color.RGBA{10, 20, 30, 255}) {
		t.Errorf("last pixel of the first frame %v", c)
	}
	if c := got[1].RGBAAt(15, 15); c != (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("last pixel of the second frame %v", c)
	}
}

func TestDecodeRejects(t *testing.T) {
	t.Parallel()
	for name, file := range map[string][]byte{
		"empty":            nil,
		"other format":     append([]byte{0x11}, make([]byte, frameBytes)...),
		"not whole blocks": append([]byte{formatAnim}, make([]byte, frameBytes+4)...),
		"too short":        append([]byte{formatAnim}, make([]byte, 30)...),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := Decode(file); !errors.Is(err, ErrFormat) {
				t.Errorf("err %v, want ErrFormat", err)
			}
		})
	}
}

func TestGIFKeepsColoursAndTiming(t *testing.T) {
	t.Parallel()
	a := image.NewRGBA(image.Rect(0, 0, 16, 16))
	b := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := range 256 {
		a.SetRGBA(i%16, i/16, color.RGBA{uint8(i), 0, 0, 255})
		b.SetRGBA(i%16, i/16, color.RGBA{0, 0, 200, 255})
	}
	out, err := GIF([]*image.RGBA{a, b}, 123*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 2 || g.Delay[0] != 12 {
		t.Fatalf("frames %d delay %v", len(g.Image), g.Delay)
	}
	r, _, _, _ := g.Image[0].At(7, 3).RGBA()
	if uint8(r>>8) != 7+3*16 {
		t.Errorf("pixel red %d, want %d", r>>8, 7+3*16)
	}
	if _, err := GIF(nil, time.Second); !errors.Is(err, ErrFormat) {
		t.Errorf("empty frames: %v", err)
	}
}

func TestListAndDownload(t *testing.T) {
	t.Parallel()
	var got map[string]int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/GetCategoryFileListV2":
			body, _ := io.ReadAll(r.Body)
			json.Unmarshal(body, &got)
			w.Write([]byte(`{"ReturnCode":0,"FileList":[{"GalleryId":7,"FileName":"ghost","UserName":"someone","FileTagArray":["halloween"],"LikeCnt":3,"FileId":"group1/M00/x"}]}`))
		case "/group1/M00/x":
			w.Write([]byte{formatAnim, 1, 2, 3})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), API: srv.URL, Files: srv.URL}

	items, err := c.List(context.Background(), Categories["recommended"], MostLiked, 2, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != 7 || items[0].FileID != "group1/M00/x" || items[0].Tags[0] != "halloween" {
		t.Errorf("items %+v", items)
	}
	if got["StartNum"] != 21 || got["EndNum"] != 40 || got["Classify"] != 18 || got["FileSize"] != 1 || got["FileSort"] != 1 {
		t.Errorf("request %v", got)
	}

	file, err := c.Download(context.Background(), items[0].FileID)
	if err != nil || !bytes.Equal(file, []byte{formatAnim, 1, 2, 3}) {
		t.Errorf("download %v %v", file, err)
	}
	if _, err := c.Download(context.Background(), "../etc/passwd"); err == nil {
		t.Error("a path with .. was fetched")
	}
}

func TestListReportsAPIErrors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ReturnCode":11,"ReturnMessage":"Token is not match"}`))
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), API: srv.URL, Files: srv.URL}
	if _, err := c.List(context.Background(), 0, Newest, 1, 5); err == nil {
		t.Error("a non-zero ReturnCode was accepted")
	}
}
