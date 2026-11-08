package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/drawings"
	"github.com/PlatanosVerdes/pixelhub/internal/lists"
)

func serve(t *testing.T, srv http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestDrawings(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	dir := t.TempDir()
	store, err := drawings.Open(dir, 16)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := lists.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := (&Server{Hub: f.hub, Drawings: store, Lists: kept, Size: 16}).Handler()
	red, black := strings.Repeat("ff0000", 256), strings.Repeat("000000", 256)

	rec := serve(t, srv, "POST", "/api/drawings", fmt.Sprintf(`{"name":"  blink ","delay_ms":200,"frames":[%q,%q]}`, red, black))
	var got struct {
		Drawing  drawings.Drawing   `json:"drawing"`
		Drawings []drawings.Drawing `json:"drawings"`
	}
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Drawing.ID == "" || got.Drawing.Name != "blink" || len(got.Drawings) != 1 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	id := got.Drawing.ID

	for name, body := range map[string]string{
		"short frame": `{"name":"x","delay_ms":200,"frames":["ff0000"]}`,
		"no frames":   `{"name":"x","delay_ms":200,"frames":[]}`,
		"too fast":    fmt.Sprintf(`{"name":"x","delay_ms":5,"frames":[%q]}`, red),
		"no name":     fmt.Sprintf(`{"name":" ","delay_ms":200,"frames":[%q]}`, red),
		"not hex":     fmt.Sprintf(`{"name":"x","delay_ms":200,"frames":[%q]}`, strings.Repeat("zz0000", 256)),
	} {
		if rec := serve(t, srv, "POST", "/api/drawings", body); rec.Code != 400 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}

	if rec := serve(t, srv, "PUT", "/api/drawings/"+id, fmt.Sprintf(`{"name":"red","delay_ms":100,"frames":[%q]}`, red)); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"red"`) {
		t.Errorf("update: %d %s", rec.Code, rec.Body)
	}
	if rec := serve(t, srv, "PUT", "/api/drawings/nope", fmt.Sprintf(`{"name":"red","delay_ms":100,"frames":[%q]}`, red)); rec.Code != 404 {
		t.Errorf("update a missing one: %d %s", rec.Code, rec.Body)
	}

	if rec := serve(t, srv, "POST", "/api/drawings/show", fmt.Sprintf(`{"name":"live","delay_ms":100,"frames":[%q,%q],"for":"5s"}`, red, black)); rec.Code != 202 {
		t.Fatalf("show: %d %s", rec.Code, rec.Body)
	}
	for deadline := time.Now().Add(2 * time.Second); f.hub.Status().Showing != "live"; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("showing %q, want live", f.hub.Status().Showing)
		}
	}

	if rec := serve(t, srv, "DELETE", "/api/show", ""); rec.Code != 200 {
		t.Errorf("unshow: %d %s", rec.Code, rec.Body)
	}
	for deadline := time.Now().Add(2 * time.Second); f.hub.Status().Showing == "live"; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("still showing the drawing after DELETE /api/show")
		}
	}

	if rec := serve(t, srv, "POST", "/api/drawings/"+id+"/list", `{"list":"favorites"}`); rec.Code != 201 || !strings.Contains(rec.Body.String(), `"file":"drawing/`+id+`"`) {
		t.Errorf("into a list: %d %s", rec.Code, rec.Body)
	}
	if _, ok, err := kept.Pick("favorites", 0, 16); !ok || err != nil {
		t.Errorf("the list does not play it: %v %v", ok, err)
	}

	if rec := serve(t, srv, "DELETE", "/api/drawings/"+id, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"drawings":[]`) {
		t.Errorf("delete: %d %s", rec.Code, rec.Body)
	}
	again, err := drawings.Open(dir, 16)
	if err != nil || len(again.List()) != 0 {
		t.Errorf("reopened: %v %v", again.List(), err)
	}
}
