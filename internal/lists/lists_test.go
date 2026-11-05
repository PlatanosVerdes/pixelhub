package lists

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"os"
	"testing"
)

func TestLists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Lists() == nil {
		t.Error("no lists should be [], not nil")
	}
	for _, name := range []string{"Halloween", "  Cats   and dogs "} {
		if err := s.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Create("halloween"); !errors.Is(err, ErrExists) {
		t.Errorf("a name differing only in case: %v", err)
	}
	pumpkin := Picture{File: "group1/M00/pumpkin", Name: "pumpkin"}
	if err := s.Add("Halloween", pumpkin, []byte("GIF89a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("Cats and dogs", pumpkin, []byte("GIF89a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("Halloween", Picture{File: "group1/M00/ghost"}, []byte("GIF89a")); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Lists()
	if len(got) != 2 || got[0].Name != "Halloween" || got[1].Name != "Cats and dogs" || len(got[0].Items) != 2 || got[0].Items[0].File != "group1/M00/ghost" {
		t.Fatalf("after a restart: %+v", got)
	}

	if err := reopened.Remove("Halloween", "group1/M00/pumpkin"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.GIF("group1/M00/pumpkin"); err != nil {
		t.Errorf("another list still holds it, its GIF must stay: %v", err)
	}
	if err := reopened.Delete("Cats and dogs"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reopened.gifPath("group1/M00/pumpkin")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no list holds it any more, its GIF should be gone: %v", err)
	}
	if err := reopened.Rename("Halloween", "Spooky"); err != nil || reopened.Lists()[0].Name != "Spooky" {
		t.Errorf("rename: %v %+v", err, reopened.Lists())
	}
}

func TestListsReject(t *testing.T) {
	t.Parallel()
	s, _ := Open(t.TempDir())
	if err := s.Create("   "); !errors.Is(err, ErrInvalid) {
		t.Errorf("blank name: %v", err)
	}
	if err := s.Add("nope", Picture{File: "x"}, []byte("g")); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown list: %v", err)
	}
	_ = s.Create("a")
	_ = s.Create("b")
	if err := s.Rename("a", "B"); !errors.Is(err, ErrExists) {
		t.Errorf("rename onto another list: %v", err)
	}
	if err := s.Remove("a", "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove a missing picture: %v", err)
	}
}

func TestPick(t *testing.T) {
	t.Parallel()
	s, _ := Open(t.TempDir())
	if _, ok, err := s.Pick("none", 0, 16); ok || err != nil {
		t.Errorf("a missing list picks nothing: %v %v", ok, err)
	}
	_ = s.Create("Night")
	for _, name := range []string{"moon", "owl"} {
		var b bytes.Buffer
		img := image.NewPaletted(image.Rect(0, 0, 16, 16), color.Palette{color.Black, color.White})
		_ = gif.Encode(&b, img, nil)
		if err := s.Add("Night", Picture{File: "f/" + name, Name: name}, b.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	for turn, want := range []string{"owl", "moon", "owl"} {
		c, ok, err := s.Pick("night", turn, 16)
		if err != nil || !ok || c.Name != want || c.Key != "f/"+want || len(c.Frames) != 1 {
			t.Errorf("turn %d: %q %v %v, want %s", turn, c.Name, ok, err, want)
		}
	}
}
