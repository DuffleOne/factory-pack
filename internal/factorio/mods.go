package factorio

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Mod is one mod the game can load: one that ships with it, like base and
// space-age, or one of the player's.
type Mod struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Title   string `json:"title,omitempty"`
	// Path is the mod's zip or folder. Empty for one that ships with the
	// game, which sits in its data folder.
	Path    string `json:"path,omitempty"`
	Builtin bool   `json:"builtin,omitempty"`
	// Enabled is whether the game has it switched on now.
	Enabled bool `json:"enabled"`
	// Requires is what has to be loaded with it, by name.
	Requires []string `json:"requires,omitempty"`
}

// ModsDir is where the player's mods are.
func (in *Install) ModsDir() string { return filepath.Join(in.User, "mods") }

// Mods is every mod this copy of the game could load: what ships with it,
// base first, then the player's by name, the newest of each where there
// are several versions. Enabled says what the game has switched on.
func (in *Install) Mods() ([]Mod, error) {
	on, listed := enabled(filepath.Join(in.ModsDir(), "mod-list.json"))

	var out []Mod

	shipped, err := os.ReadDir(in.Data)
	if err != nil {
		return nil, err
	}

	for _, e := range shipped {
		// core is the game itself rather than a mod anyone switches off.
		if !e.IsDir() || e.Name() == "core" {
			continue
		}

		info, err := readInfo(filepath.Join(in.Data, e.Name()))
		if err != nil {
			continue
		}

		m := info.mod("")
		m.Builtin = true
		// The game switches on what ships with it unless told otherwise.
		m.Enabled = m.Name == "base" || on[m.Name] || !listed[m.Name]
		out = append(out, m)
	}

	slices.SortFunc(out, func(a, b Mod) int {
		if (a.Name == "base") != (b.Name == "base") {
			if a.Name == "base" {
				return -1
			}

			return 1
		}

		return strings.Compare(a.Name, b.Name)
	})

	theirs := map[string]Mod{}

	entries, err := os.ReadDir(in.ModsDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	for _, e := range entries {
		path := filepath.Join(in.ModsDir(), e.Name())
		if !e.IsDir() && !strings.EqualFold(filepath.Ext(e.Name()), ".zip") {
			continue
		}

		info, err := readInfo(path)
		if err != nil || info.Name == "" {
			continue
		}

		m := info.mod(path)
		if old, ok := theirs[m.Name]; ok && !newer(m.Version, old.Version) {
			continue
		}

		m.Enabled = on[m.Name]
		theirs[m.Name] = m
	}

	names := make([]string, 0, len(theirs))
	for n := range theirs {
		names = append(names, n)
	}

	slices.SortFunc(names, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })

	for _, n := range names {
		out = append(out, theirs[n])
	}

	return out, nil
}

// Pick is the mods named, base always among them, and with needs set
// everything they need to load, found among all. A name that isn't there
// is an error, and so is something needed that isn't.
func Pick(all []Mod, names []string, needs bool) ([]Mod, error) {
	byName := map[string]Mod{}
	for _, m := range all {
		byName[m.Name] = m
	}

	picked := map[string]bool{}

	var add func(name, by string) error

	add = func(name, by string) error {
		if picked[name] {
			return nil
		}

		m, ok := byName[name]
		if !ok {
			if by != "" {
				return fmt.Errorf("%s needs %s, which isn't in your mods folder", by, name)
			}

			return fmt.Errorf("there's no mod called %s in your mods folder", name)
		}

		picked[name] = true

		if !needs {
			return nil
		}

		for _, r := range m.Requires {
			if err := add(r, name); err != nil {
				return err
			}
		}

		return nil
	}

	for _, n := range append([]string{"base"}, names...) {
		if err := add(strings.TrimSpace(n), ""); err != nil {
			return nil, err
		}
	}

	var out []Mod

	for _, m := range all {
		if picked[m.Name] {
			out = append(out, m)
		}
	}

	return out, nil
}

// Switched is the names of the mods the game has switched on.
func Switched(all []Mod) []string {
	var out []string

	for _, m := range all {
		if m.Enabled {
			out = append(out, m.Name)
		}
	}

	return out
}

// ModList is the mods a mod-list.json has switched on, by name: a server's,
// say, to make a pack of the game it runs.
func ModList(path string) ([]string, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	on, listed := enabled(path)
	if len(listed) == 0 {
		return nil, fmt.Errorf("%s names no mods", path)
	}

	var out []string

	for name, ok := range on {
		if ok {
			out = append(out, name)
		}
	}

	slices.Sort(out)

	return out, nil
}

// enabled reads a mod-list.json: which mods are on, and which it names at
// all. A missing file names none.
func enabled(path string) (on, listed map[string]bool) {
	on, listed = map[string]bool{}, map[string]bool{}

	b, err := os.ReadFile(path)
	if err != nil {
		return on, listed
	}

	var list struct {
		Mods []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"mods"`
	}

	if json.Unmarshal(b, &list) != nil {
		return on, listed
	}

	for _, m := range list.Mods {
		listed[m.Name], on[m.Name] = true, m.Enabled
	}

	return on, listed
}

type modInfo struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Title        string   `json:"title"`
	Dependencies []string `json:"dependencies"`
}

func (i *modInfo) mod(path string) Mod {
	m := Mod{Name: i.Name, Version: i.Version, Title: i.Title, Path: path}

	for _, d := range i.Dependencies {
		if name, ok := required(d); ok {
			m.Requires = append(m.Requires, name)
		}
	}

	return m
}

// required reads one of a mod's dependencies: the mod's name, and whether
// it has to be loaded. "! x" is a mod it can't load beside, and "? x" and
// "(?) x" ones it can do without; a bare name, or "~ x", is needed.
func required(dep string) (string, bool) {
	dep = strings.TrimSpace(dep)

	switch {
	case strings.HasPrefix(dep, "!"), strings.HasPrefix(dep, "?"), strings.HasPrefix(dep, "(?)"):
		return "", false
	case strings.HasPrefix(dep, "~"):
		dep = strings.TrimSpace(dep[1:])
	}
	// The name runs to the comparison, if there is one. Names have no
	// spaces in them, but may have the comparison's characters in them only
	// after a space.
	name := dep
	if i := strings.IndexAny(dep, " <>="); i >= 0 {
		name = dep[:i]
	}

	return name, name != ""
}

// readInfo reads a mod's info.json, out of its folder or its zip. Inside a
// zip it sits in the one folder everything else does.
func readInfo(path string) (*modInfo, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if st.IsDir() {
		f, err := os.Open(filepath.Join(path, "info.json"))
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()

		return decodeInfo(f)
	}

	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = z.Close() }()

	for _, f := range z.File {
		if _, rest := pathSplit(f.Name); rest != "info.json" {
			continue
		}

		r, err := f.Open()
		if err != nil {
			return nil, err
		}

		info, err := decodeInfo(r)
		_ = r.Close()

		return info, err
	}

	return nil, fmt.Errorf("%s has no info.json", path)
}

// pathSplit cuts a path inside a zip at its first slash.
func pathSplit(name string) (dir, rest string) {
	dir, rest, ok := strings.Cut(name, "/")
	if !ok {
		return "", name
	}

	return dir, rest
}

func decodeInfo(r io.Reader) (*modInfo, error) {
	var info modInfo
	if err := json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&info); err != nil {
		return nil, err
	}

	return &info, nil
}

// newer says whether version a comes after b, number by number.
func newer(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(as), len(bs)) {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}

		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}

		if x != y {
			return x > y
		}
	}

	return false
}
