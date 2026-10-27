package factorio

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Scratch is a folder the game runs in. It writes there rather than into
// the player's folders, so their saves, settings and login are never
// touched or read, and an open copy of the game doesn't get in the way. Its
// mod folder holds just the mods picked.
type Scratch struct {
	Dir    string
	write  string
	mods   string
	config string
}

// Settings is which mod settings the game loads: the player's own, every
// mod's defaults, or those in a mod-settings.dat named by path.
type Settings string

const (
	Mine     Settings = "mine"
	Defaults Settings = "defaults"
)

// NewScratch makes a scratch folder under parent, the system's temporary
// folder if that's empty, that loads mods with settings and names things
// in locale ("en", "de" and so on; empty is the game's own choice).
func (in *Install) NewScratch(parent string, mods []Mod, settings Settings, locale string) (*Scratch, error) {
	dir, err := os.MkdirTemp(parent, "factory-pack-*")
	if err != nil {
		return nil, err
	}

	s := &Scratch{
		Dir:    dir,
		write:  filepath.Join(dir, "game"),
		mods:   filepath.Join(dir, "mods"),
		config: filepath.Join(dir, "config.ini"),
	}

	if err := in.fill(s, mods, settings, locale); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}

	return s, nil
}

// Close deletes the folder and everything in it. The links in its mod
// folder go, and what they point at stays.
func (s *Scratch) Close() error { return os.RemoveAll(s.Dir) }

// Output is the folder the game writes files into when asked.
func (s *Scratch) Output() string { return filepath.Join(s.write, "script-output") }

func (in *Install) fill(s *Scratch, mods []Mod, settings Settings, locale string) error {
	for _, d := range []string{s.write, s.mods} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	ini := fmt.Sprintf("[path]\nread-data=%s\nwrite-data=%s\n", in.Data, s.write)
	if locale != "" {
		ini += fmt.Sprintf("[general]\nlocale=%s\n", locale)
	}

	if err := os.WriteFile(s.config, []byte(ini), 0o644); err != nil {
		return err
	}

	type entry struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}

	var list struct {
		Mods []entry `json:"mods"`
	}

	picked := map[string]bool{}
	for _, m := range mods {
		picked[m.Name] = true
	}
	// What ships with the game has to be switched off by name, or it loads.
	shipped, err := os.ReadDir(in.Data)
	if err != nil {
		return err
	}

	for _, e := range shipped {
		if e.IsDir() && e.Name() != "core" && isFile(filepath.Join(in.Data, e.Name(), "info.json")) {
			list.Mods = append(list.Mods, entry{e.Name(), picked[e.Name()]})
		}
	}

	for _, m := range mods {
		if m.Builtin {
			continue
		}

		if err := place(m.Path, filepath.Join(s.mods, filepath.Base(m.Path))); err != nil {
			return fmt.Errorf("putting %s in the game's mod folder: %w", m.Name, err)
		}

		list.Mods = append(list.Mods, entry{m.Name, true})
	}

	slices.SortFunc(list.Mods, func(a, b entry) int { return strings.Compare(a.Name, b.Name) })

	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(s.mods, "mod-list.json"), b, 0o644); err != nil {
		return err
	}

	from := ""

	switch settings {
	case Defaults:
	case Mine, "":
		from = filepath.Join(in.ModsDir(), "mod-settings.dat")
		if !isFile(from) {
			from = ""
		}
	default:
		from = string(settings)
		if !isFile(from) {
			return fmt.Errorf("there's no mod settings file at %s", from)
		}
	}

	if from == "" {
		return nil
	}
	// A copy, not a link: the game writes the file back when it closes.
	return copyFile(from, filepath.Join(s.mods, "mod-settings.dat"))
}

// place puts a mod's zip or folder at dst, by the cheapest way that works
// here: a hard link, then a symbolic one, then a copy.
func place(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !st.IsDir() && os.Link(src, dst) == nil {
		return nil
	}

	if os.Symlink(src, dst) == nil {
		return nil
	}

	if st.IsDir() {
		return copyDir(src, dst)
	}

	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}

	return out.Close()
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		to := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0o755)
		}

		if !d.Type().IsRegular() {
			return nil
		}

		return copyFile(path, to)
	})
}

// Run starts the game in the scratch folder with args, waits for it, and
// gives back what it printed. A game that fails says why in the last of
// what it printed, which the error carries.
func (in *Install) Run(ctx context.Context, s *Scratch, args ...string) (string, error) {
	full := append([]string{"--config", s.config, "--mod-directory", s.mods}, args...)

	cmd := exec.CommandContext(ctx, in.Binary, full...)
	cmd.Dir = s.Dir
	// Once it's been stopped, don't wait long on anything it left holding
	// its output open.
	cmd.WaitDelay = 10 * time.Second
	hide(cmd)

	b, err := cmd.CombinedOutput()
	out := string(b)

	switch {
	case ctx.Err() != nil:
		return out, fmt.Errorf("the game took too long, or was stopped: %w", ctx.Err())
	case err != nil:
		return out, fmt.Errorf("the game stopped with an error: %w\n%s", err, Tail(out, 12))
	}

	return out, nil
}

// Tail is the last n lines of s.
func Tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}

// DumpName is the file --dump-data writes.
const DumpName = "data-raw-dump.json"

// Dump has the game write out every prototype it loaded, mods and their
// settings applied, and gives back where it put them.
func (in *Install) Dump(ctx context.Context, s *Scratch) (string, error) {
	out, err := in.Run(ctx, s, "--dump-data")
	if err != nil {
		return "", err
	}

	path := filepath.Join(s.Output(), DumpName)
	if !isFile(path) {
		return "", fmt.Errorf("the game ran but wrote no %s\n%s", DumpName, Tail(out, 12))
	}

	return path, nil
}

// Names has the game write out what everything is called, one file for
// each kind of prototype, and gives back those files.
func (in *Install) Names(ctx context.Context, s *Scratch) ([]string, error) {
	out, err := in.Run(ctx, s, "--dump-prototype-locale")
	if err != nil {
		return nil, err
	}

	files, err := filepath.Glob(filepath.Join(s.Output(), "*-locale.json"))
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("the game ran but wrote no names\n%s", Tail(out, 12))
	}

	slices.Sort(files)

	return files, nil
}

//go:embed fluidboxes.lua
var fluidboxScript string

// FluidboxName is the file the fluid box scenario writes.
const FluidboxName = "factory-pack-fluidboxes.json"

const scenario = "factory-pack"

// Fluidboxes has the game set every fluid recipe on every machine that
// takes one, and write down where each fluid connects. A scenario does it,
// as it's made into a map; if that version of the game doesn't run it
// then, it does when the map is loaded.
func (in *Install) Fluidboxes(ctx context.Context, s *Scratch) (string, error) {
	dir := filepath.Join(s.write, "scenarios", scenario)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	if err := os.WriteFile(filepath.Join(dir, "control.lua"), []byte(fluidboxScript), 0o644); err != nil {
		return "", err
	}

	out, err := in.Run(ctx, s, "--scenario2map", scenario)
	if err != nil {
		return "", err
	}

	path := filepath.Join(s.Output(), FluidboxName)
	if isFile(path) {
		return path, nil
	}

	args := []string{"--benchmark", filepath.Join(s.write, "saves", scenario+".zip"), "--benchmark-ticks", "2", "--benchmark-runs", "1"}
	// The headless build has no sound to switch off, and won't start if
	// asked to.
	if !in.Headless {
		args = append(args, "--disable-audio")
	}

	if out, err = in.Run(ctx, s, args...); err != nil {
		return "", err
	}

	if !isFile(path) {
		return "", errors.New("the game ran but didn't say where fluids connect\n" + Tail(out, 12))
	}

	return path, nil
}
