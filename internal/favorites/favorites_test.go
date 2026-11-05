package favorites

import (
	"errors"
	"testing"
)

func TestAddListRemove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add(Favorite{File: "group1/M00/a", Name: "cat"}, []byte("GIF89a-cat")); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(Favorite{File: "group1/M00/b", Name: "dog"}, []byte("GIF89a-dog")); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(Favorite{File: "group1/M00/a", Name: "cat", Likes: 9}, []byte("GIF89a-cat")); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	list := reopened.List()
	if len(list) != 2 || list[0].File != "group1/M00/a" || list[0].Likes != 9 || list[0].Added.IsZero() {
		t.Fatalf("list after a restart: %+v", list)
	}
	if gif, err := reopened.GIF("group1/M00/b"); err != nil || string(gif) != "GIF89a-dog" {
		t.Errorf("gif %q, %v", gif, err)
	}
	if err := reopened.Remove("group1/M00/b"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.GIF("group1/M00/b"); !errors.Is(err, ErrNotFound) {
		t.Errorf("removed gif: %v", err)
	}
	if err := reopened.Remove("group1/M00/b"); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing twice: %v", err)
	}
	if err := reopened.Add(Favorite{}, nil); err == nil {
		t.Error("want an error for an empty favourite")
	}
}
