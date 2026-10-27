package pack

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/duffleone/factory-pack/internal/factorio"
)

var png = append([]byte("\x89PNG\r\n\x1a\n"), "pretend pixels"...)

func write(t *testing.T, path string, b []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// game is a pretend install: base in its data folder, one mod zipped and
// one in a folder, and a secret where no picture should be.
func game(t *testing.T) (*factorio.Install, []factorio.Mod) {
	t.Helper()

	root := t.TempDir()
	in := &factorio.Install{Data: filepath.Join(root, "data"), User: filepath.Join(root, "user")}

	write(t, filepath.Join(in.Data, "base", "graphics", "icons", "iron-plate.png"), png)
	write(t, filepath.Join(in.Data, "base", "graphics", "readme.txt"), []byte("not a picture"))
	write(t, filepath.Join(in.Data, "base", "graphics", "fake.png"), []byte("not a png inside"))
	write(t, filepath.Join(in.Data, "core", "graphics", "cursor.png"), png)
	write(t, filepath.Join(in.User, "player-data.json"), []byte(`{"service-token":"secret"}`))
	write(t, filepath.Join(in.User, "mods", "folder-mod", "graphics", "a.png"), png)
	write(t, filepath.Join(in.User, "mods", "Flare Stack_4.1.1", "graphics", "flare.png"), png)

	zipPath := filepath.Join(in.User, "mods", "zipped_1.2.3.zip")

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)
	for name, b := range map[string][]byte{"zipped_1.2.3/info.json": []byte(`{"name":"zipped","version":"1.2.3"}`), "zipped_1.2.3/graphics/b.png": png} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		_, _ = w.Write(b)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	write(t, zipPath, buf.Bytes())

	mods := []factorio.Mod{
		{Name: "base", Builtin: true},
		{Name: "folder-mod", Path: filepath.Join(in.User, "mods", "folder-mod")},
		{Name: "zipped", Path: zipPath},
		{Name: "Flare Stack", Path: filepath.Join(in.User, "mods", "Flare Stack_4.1.1")},
	}

	return in, mods
}

func TestPicturesReadsOnlyGamePictures(t *testing.T) {
	in, mods := game(t)
	p := NewPictures(in, mods)

	defer p.Close()

	for _, ok := range []string{
		"__base__/graphics/icons/iron-plate.png",
		"__core__/graphics/cursor.png",
		"__folder-mod__/graphics/a.png",
		"__zipped__/graphics/b.png",
		"__Flare Stack__/graphics/flare.png",
	} {
		if b, err := p.Read(ok); err != nil || !bytes.Equal(b, png) {
			t.Errorf("%s: %v", ok, err)
		}
	}

	for _, bad := range []string{
		"__base__/graphics/readme.txt",
		"__base__/graphics/fake.png",
		"__base__/../player-data.json",
		"__base__/../../user/player-data.json.png",
		"__base__/graphics/../../../user/x.png",
		"__base__//graphics/icons/iron-plate.png",
		"__base__/./graphics/icons/iron-plate.png",
		"__base__/graphics\\icons\\iron-plate.png",
		"__base__/C:/Windows/x.png",
		"/etc/passwd.png",
		"__not-picked__/graphics/a.png",
		"__..__/x.png",
		"__ __/x.png",
		"__base__/graphics/icons/iron-plate.png\x00.png",
		"player-data.json",
	} {
		_, err := p.Read(bad)
		if err == nil {
			t.Errorf("%q was read", bad)
			continue
		}

		if !errors.Is(err, ErrRefused) && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%q: %v, want it refused or not found", bad, err)
		}
	}
}

func TestPicturesWontFollowLinks(t *testing.T) {
	in, mods := game(t)
	secret := filepath.Join(in.User, "player-data.json")

	link := filepath.Join(in.Data, "base", "graphics", "sneaky.png")
	if err := os.Symlink(secret, link); err != nil {
		t.Skip("no symlinks here")
	}

	p := NewPictures(in, mods)
	defer p.Close()

	if _, err := p.Read("__base__/graphics/sneaky.png"); !errors.Is(err, ErrRefused) {
		t.Errorf("a link out of the game was followed: %v", err)
	}
}

func TestExportIsTheSameEachTime(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "dump.json"), []byte(`{"item":{}}`))

	parts := []part{{name: "manifest.json", data: []byte(`{"format":1}`)}, {name: "data-raw-dump.json", path: filepath.Join(dir, "dump.json")}}

	if _, err := writeExport(filepath.Join(dir, "a.tar.gz"), parts); err != nil {
		t.Fatal(err)
	}

	if _, err := writeExport(filepath.Join(dir, "b.tar.gz"), parts); err != nil {
		t.Fatal(err)
	}

	a, _ := os.ReadFile(filepath.Join(dir, "a.tar.gz"))
	b, _ := os.ReadFile(filepath.Join(dir, "b.tar.gz"))

	if !bytes.Equal(a, b) {
		t.Error("the same files packed twice came out different")
	}
}

// planner is a pretend planner, taking a pack the way the real one does.
type planner struct {
	mu       sync.Mutex
	got      map[string][]byte
	batches  int
	built    bool
	wants    []string
	tokenSaw []string
}

func (p *planner) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/packs":
		_, _ = io.Copy(io.Discard, r.Body)
		_ = json.NewEncoder(w).Encode(Pack{Code: "ABCD2345", State: "uploading", Token: "tok", Pictures: p.wants, Batch: 20})
	case r.Method == http.MethodPost && r.URL.Path == "/api/packs/ABCD2345/pictures":
		p.tokenSaw = append(p.tokenSaw, r.Header.Get("Authorization"))
		p.batches++

		tr := tar.NewReader(r.Body)
		for {
			h, err := tr.Next()
			if err != nil {
				break
			}

			b, _ := io.ReadAll(tr)
			p.got[h.Name] = b
		}

		_, _ = w.Write([]byte(`{"kept":1}`))
	case r.Method == http.MethodPost && r.URL.Path == "/api/packs/ABCD2345/build":
		p.built = true
		_ = json.NewEncoder(w).Encode(Pack{Code: "ABCD2345", State: "building"})
	case r.Method == http.MethodGet && r.URL.Path == "/api/packs/ABCD2345":
		state := "building"
		if p.built {
			state = "ready"
		}

		_ = json.NewEncoder(w).Encode(Pack{Code: "ABCD2345", State: state, Name: "test"})
	default:
		http.Error(w, `{"error":"no"}`, http.StatusNotFound)
	}
}

func TestSend(t *testing.T) {
	in, mods := game(t)
	pics := NewPictures(in, mods)

	defer pics.Close()

	p := &planner{got: map[string][]byte{}, wants: []string{
		"__base__/graphics/icons/iron-plate.png",
		"__zipped__/graphics/b.png",
		"__base__/../player-data.json",
		"__base__/graphics/missing.png",
	}}

	srv := httptest.NewServer(p)
	defer srv.Close()

	export := filepath.Join(t.TempDir(), "export.tar.gz")
	write(t, export, []byte("pretend export"))

	var kept []string

	c := &Client{Server: srv.URL, Poll: 1}

	sent, err := c.Send(context.Background(), export, pics, nil, func(name string, _ []byte) error {
		kept = append(kept, name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if sent.Code != "ABCD2345" || sent.State != "ready" {
		t.Errorf("came to %+v", sent.Pack)
	}

	if len(p.got) != 2 || p.got["__zipped__/graphics/b.png"] == nil {
		t.Errorf("the planner got %d pictures", len(p.got))
	}

	if p.batches != 2 {
		t.Errorf("%d batches for two pictures over a batch's size, want 2", p.batches)
	}

	for _, auth := range p.tokenSaw {
		if auth != "Bearer tok" {
			t.Errorf("pictures went with %q", auth)
		}
	}

	if len(sent.Missing) != 2 || !strings.Contains(sent.Missing["__base__/../player-data.json"], "not a picture this tool sends") {
		t.Errorf("missing: %v", sent.Missing)
	}

	if len(kept) != 2 {
		t.Errorf("kept copies of %v", kept)
	}
}
