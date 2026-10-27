package pack

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/duffleone/factory-pack/internal/factorio"
)

// MaxPicture is the biggest picture that goes up. The game's own biggest
// are a few megabytes.
const MaxPicture = 32 << 20

// gamePath is how prototypes name a file: "__mod__/graphics/thing.png". A
// mod's name can have spaces in it, as "Flare Stack" does. Whatever it is,
// it has to be one of the mods picked before anything is read.
var gamePath = regexp.MustCompile(`^__([A-Za-z0-9_. -]+)__/(.+)$`)

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// ErrRefused is a picture asked for that this tool won't send, whatever
// the server says: anything but a PNG out of the game or the mods picked.
var ErrRefused = errors.New("not a picture this tool sends")

// Pictures reads the pictures the server asks for. It only ever reads
// PNGs, by the paths prototypes use, out of the game's own folder for what
// ships with it and out of the mods picked for theirs. Anything else is
// refused before a file is opened, so nothing else on the machine can be
// asked for: not saves, not settings, not the game's login.
type Pictures struct {
	data string
	mods map[string]factorio.Mod
	zips map[string]*zipIndex
}

type zipIndex struct {
	z     *zip.ReadCloser
	files map[string]*zip.File
}

// NewPictures reads pictures for an export of mods from the game at in.
func NewPictures(in *factorio.Install, mods []factorio.Mod) *Pictures {
	p := &Pictures{data: in.Data, mods: map[string]factorio.Mod{}, zips: map[string]*zipIndex{}}
	for _, m := range mods {
		p.mods[m.Name] = m
	}
	// core isn't a mod anyone picks, but every game loads it.
	p.mods["core"] = factorio.Mod{Name: "core", Builtin: true}

	return p
}

// Close closes the mod zips opened.
func (p *Pictures) Close() {
	for _, z := range p.zips {
		if z != nil {
			_ = z.z.Close()
		}
	}
}

// Read is the picture a prototype names by name.
func (p *Pictures) Read(name string) ([]byte, error) {
	mod, rest, err := split(name)
	if err != nil {
		return nil, err
	}

	m, ok := p.mods[mod]
	if !ok {
		return nil, fmt.Errorf("%s: %s isn't one of the mods in this pack: %w", name, mod, ErrRefused)
	}

	var b []byte

	switch {
	case m.Builtin:
		b, err = readCapped(filepath.Join(p.data, mod, filepath.FromSlash(rest)))
	case isDir(m.Path):
		b, err = readCapped(filepath.Join(m.Path, filepath.FromSlash(rest)))
	default:
		b, err = p.fromZip(m, rest)
	}

	if err != nil {
		return nil, err
	}

	if !bytes.HasPrefix(b, pngSignature) {
		return nil, fmt.Errorf("%s isn't a PNG: %w", name, ErrRefused)
	}

	return b, nil
}

// split checks a name is a PNG by a prototype's path, inside its mod, and
// cuts it into the mod and the path within it.
func split(name string) (mod, rest string, err error) {
	m := gamePath.FindStringSubmatch(name)
	if m == nil {
		return "", "", fmt.Errorf("%q isn't a path a prototype would use: %w", name, ErrRefused)
	}

	mod, rest = m[1], m[2]
	if !strings.EqualFold(path.Ext(rest), ".png") {
		return "", "", fmt.Errorf("%s isn't a PNG: %w", name, ErrRefused)
	}
	// Nothing that climbs out of the mod, starts from a root, or hides a
	// separator or a NUL in itself.
	if path.Clean(rest) != rest || strings.HasPrefix(rest, "/") || strings.ContainsAny(rest, "\\\x00:") {
		return "", "", fmt.Errorf("%q isn't a plain path: %w", name, ErrRefused)
	}

	for _, seg := range strings.Split(rest, "/") {
		if seg == ".." || seg == "." || seg == "" {
			return "", "", fmt.Errorf("%q isn't a plain path: %w", name, ErrRefused)
		}
	}

	return mod, rest, nil
}

func (p *Pictures) fromZip(m factorio.Mod, rest string) ([]byte, error) {
	ix, ok := p.zips[m.Name]
	if !ok {
		z, err := zip.OpenReader(m.Path)
		if err != nil {
			p.zips[m.Name] = nil
			return nil, err
		}

		ix = &zipIndex{z: z, files: map[string]*zip.File{}}
		// Everything in a mod's zip sits under one folder, named for the
		// mod with or without its version.
		for _, f := range z.File {
			if _, inside, ok := strings.Cut(f.Name, "/"); ok && inside != "" {
				ix.files[inside] = f
			}
		}

		p.zips[m.Name] = ix
	}

	if ix == nil {
		return nil, fmt.Errorf("%s's zip can't be read", m.Name)
	}

	f, ok := ix.files[rest]
	if !ok {
		return nil, fmt.Errorf("%s has no %s: %w", m.Name, rest, os.ErrNotExist)
	}

	if f.UncompressedSize64 > MaxPicture {
		return nil, fmt.Errorf("%s in %s is too big to send", rest, m.Name)
	}

	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()

	return readAll(r)
}

func readCapped(file string) ([]byte, error) {
	st, err := os.Lstat(file)
	if err != nil {
		return nil, err
	}
	// A link could point anywhere. The game's own pictures aren't links.
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s isn't a plain file: %w", file, ErrRefused)
	}

	if st.Size() > MaxPicture {
		return nil, fmt.Errorf("%s is too big to send", file)
	}

	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	return readAll(f)
}

func readAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxPicture+1))
	if err != nil {
		return nil, err
	}

	if len(b) > MaxPicture {
		return nil, errors.New("too big to send")
	}

	return b, nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
