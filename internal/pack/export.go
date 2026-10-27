// Package pack turns what the game writes about a set of mods into an
// export, and sends it to the factory planner for a code.
//
// An export is a gzipped tar holding:
//
//	manifest.json       what's in it and what made it
//	data-raw-dump.json  every prototype, as the game's --dump-data writes it
//	names/*.json        what everything is called, from --dump-prototype-locale
//	fluidboxes.json     where fluids connect on each machine, recipe by recipe
//
// Nothing in it is a file from the player's folders: it's all written by
// the game, in a folder of its own, for this. Pictures go up afterwards,
// one at a time as the server names them, and only PNGs out of the game
// and the mods picked.
package pack

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/duffleone/factory-pack/internal/factorio"
)

// Format is the version of the export this tool writes.
const Format = 1

// Manifest says what an export holds and what made it. It has no times in
// it: the same game, mods and settings make the same export, byte for
// byte, so they get the same code.
type Manifest struct {
	Format int    `json:"format"`
	Tool   string `json:"tool"`
	// Game is the version of the game that wrote it.
	Game string `json:"game"`
	// Name is what the pack is called in the planner.
	Name string `json:"name"`
	// Locale is the language things are named in.
	Locale string `json:"locale,omitempty"`
	// Settings is which mod settings were loaded: mine, defaults or file.
	Settings string `json:"settings"`
	Mods     []Mod  `json:"mods"`
}

// Mod is one mod the game loaded.
type Mod struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Title   string `json:"title,omitempty"`
}

// Options say what to make.
type Options struct {
	// Name is what the pack is called in the planner.
	Name string
	// Mods are the mods to load, base among them.
	Mods []factorio.Mod
	// Settings are the mod settings to load them with.
	Settings factorio.Settings
	// Locale is the language to name things in, empty for the game's own.
	Locale string
	// Work is where the game runs, the system's temporary folder if empty.
	Work string
	// Out is the file the export is written to.
	Out string
	// Tool is what's making it, as "factory-pack 1.0.0".
	Tool string
}

// Step is how far along making or sending a pack has got.
type Step struct {
	// Key says which step it is, for a window that lists them: note, dump,
	// names, fluids, pack, export, pictures and build.
	Key string `json:"key"`
	// Stage is a short line on what's happening now.
	Stage string `json:"stage"`
	// Done and Of count the parts of the stage, when it has parts, and
	// Bytes says they're bytes rather than things.
	Done  int64 `json:"done,omitempty"`
	Of    int64 `json:"of,omitempty"`
	Bytes bool  `json:"bytes,omitempty"`
}

// Export is a finished export, on disk.
type Export struct {
	Path     string   `json:"path"`
	Size     int64    `json:"size"`
	Manifest Manifest `json:"manifest"`
	// Files is everything in it, with how big each is unpacked.
	Files []File `json:"files"`
}

// File is one file in an export, or one picture sent.
type File struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Make runs the game three times in a scratch folder, to have it write out
// its prototypes, their names, and where fluids connect, and packs what it
// wrote into an export at opt.Out. The scratch folder goes when it's done.
func Make(ctx context.Context, in *factorio.Install, opt Options, progress func(Step)) (*Export, error) {
	if progress == nil {
		progress = func(Step) {}
	}

	s, err := in.NewScratch(opt.Work, opt.Mods, opt.Settings, opt.Locale)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	progress(Step{Key: "dump", Stage: "loading your mods in the game for its data", Done: 1, Of: 3})

	dump, err := in.Dump(ctx, s)
	if err != nil {
		return nil, err
	}

	progress(Step{Key: "names", Stage: "loading them again for what everything is called", Done: 2, Of: 3})

	names, err := in.Names(ctx, s)
	if err != nil {
		return nil, err
	}

	progress(Step{Key: "fluids", Stage: "and once more for where pipes join each machine", Done: 3, Of: 3})

	boxes, err := in.Fluidboxes(ctx, s)
	if err != nil {
		return nil, err
	}

	m := Manifest{Format: Format, Tool: opt.Tool, Game: in.Version, Name: opt.Name, Locale: opt.Locale, Settings: settingsKind(opt.Settings)}
	for _, mod := range opt.Mods {
		m.Mods = append(m.Mods, Mod{Name: mod.Name, Version: mod.Version, Title: mod.Title})
	}

	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}

	progress(Step{Key: "pack", Stage: "packing up what the game wrote"})

	parts := []part{{name: "manifest.json", data: manifest}, {name: "data-raw-dump.json", path: dump}}
	for _, n := range names {
		parts = append(parts, part{name: "names/" + filepath.Base(n), path: n})
	}

	parts = append(parts, part{name: "fluidboxes.json", path: boxes})

	files, err := writeExport(opt.Out, parts)
	if err != nil {
		return nil, err
	}

	st, err := os.Stat(opt.Out)
	if err != nil {
		return nil, err
	}

	return &Export{Path: opt.Out, Size: st.Size(), Manifest: m, Files: files}, nil
}

func settingsKind(s factorio.Settings) string {
	switch s {
	case factorio.Defaults:
		return "defaults"
	case factorio.Mine, "":
		return "mine"
	default:
		return "file"
	}
}

// part is one file to go in an export, from memory or from disk.
type part struct {
	name string
	data []byte
	path string
}

// writeExport packs parts into a gzipped tar at path, in the order given,
// with nothing in the headers that would differ between two runs.
func writeExport(path string, parts []part) ([]File, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	files, err := pack(f, parts)
	if cerr := f.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}

	return files, nil
}

func pack(w io.Writer, parts []part) ([]File, error) {
	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return nil, err
	}

	tw := tar.NewWriter(zw)

	var files []File

	for _, p := range parts {
		var (
			r    io.Reader
			size int64
		)

		if p.path != "" {
			src, err := os.Open(p.path)
			if err != nil {
				return nil, err
			}
			defer func() { _ = src.Close() }()

			st, err := src.Stat()
			if err != nil {
				return nil, err
			}

			r, size = src, st.Size()
		} else {
			r, size = bytes.NewReader(p.data), int64(len(p.data))
		}

		hdr := &tar.Header{Name: p.name, Mode: 0o644, Size: size, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}

		if _, err := io.Copy(tw, r); err != nil {
			return nil, fmt.Errorf("packing %s: %w", p.name, err)
		}

		files = append(files, File{Name: p.name, Size: size})
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return files, zw.Close()
}
