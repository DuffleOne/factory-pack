// Package factorio finds a copy of Factorio on this machine, lists the mods
// it has, and runs it in a folder of its own to have it write out its data.
package factorio

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Install is one copy of the game.
type Install struct {
	// Binary is the game's executable, Data the folder holding base and
	// core, and User the folder holding the player's mods and saves.
	Binary string `json:"binary"`
	Data   string `json:"data"`
	User   string `json:"user"`
	// Version is the game's, as "2.1.24", and Headless whether it's the
	// build servers run, which has no pictures in it. Both are empty for a
	// game that won't say.
	Version  string `json:"version,omitempty"`
	Headless bool   `json:"headless,omitempty"`
}

// Paths are what to use in place of what Find would look for. Any left
// empty is looked for.
type Paths struct {
	Binary string `json:"binary,omitempty"`
	Data   string `json:"data,omitempty"`
	User   string `json:"user,omitempty"`
}

// ErrNotFound is Find with nowhere left to look.
var ErrNotFound = errors.New("no copy of Factorio found: say where its executable is")

// Find looks for the game in the usual places for this system, Steam
// libraries included, taking whatever p sets as given. FACTORIO_PATH,
// pointing at the executable, is used when p has no binary.
func Find(p Paths) (*Install, error) {
	if p.Binary == "" {
		p.Binary = os.Getenv("FACTORIO_PATH")
	}

	var roots []string
	if p.Binary != "" {
		roots = []string{rootOf(p.Binary)}
	} else {
		roots = candidates()
	}

	for _, root := range roots {
		in := &Install{Binary: cmp.Or(p.Binary, binaryIn(root)), Data: cmp.Or(p.Data, dataIn(root))}
		if !isFile(in.Binary) || !isDir(filepath.Join(in.Data, "base")) {
			continue
		}

		in.User = cmp.Or(p.User, userFor(root))
		in.Version, in.Headless = identify(in.Binary)

		return in, nil
	}

	if p.Binary != "" {
		if !isFile(p.Binary) {
			return nil, fmt.Errorf("there's no file at %s", p.Binary)
		}

		return nil, fmt.Errorf("%s doesn't look like Factorio: there's no base folder in %s", p.Binary, cmp.Or(p.Data, dataIn(rootOf(p.Binary))))
	}

	return nil, ErrNotFound
}

// rootOf is the game's own folder, given its executable: the one holding
// bin and data, or on a Mac the app.
func rootOf(binary string) string {
	if runtime.GOOS == "darwin" {
		// factorio.app/Contents/MacOS/factorio
		return filepath.Dir(filepath.Dir(filepath.Dir(binary)))
	}
	// Factorio/bin/x64/factorio
	return filepath.Dir(filepath.Dir(filepath.Dir(binary)))
}

func binaryIn(root string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(root, "Contents", "MacOS", "factorio")
	case "windows":
		return filepath.Join(root, "bin", "x64", "factorio.exe")
	default:
		return filepath.Join(root, "bin", "x64", "factorio")
	}
}

func dataIn(root string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(root, "Contents", "data")
	}

	return filepath.Join(root, "data")
}

// candidates is everywhere the game might be, most likely first.
func candidates() []string {
	home, _ := os.UserHomeDir()

	var out, steams []string

	switch runtime.GOOS {
	case "darwin":
		steams = []string{filepath.Join(home, "Library", "Application Support", "Steam")}
	case "windows":
		steams = []string{
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Steam"),
			filepath.Join(os.Getenv("ProgramFiles"), "Steam"),
		}
	default:
		steams = []string{
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".local", "share", "Steam"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
		}
	}

	for _, steam := range steams {
		for _, lib := range libraries(steam) {
			name := "Factorio"
			if runtime.GOOS == "darwin" {
				name = filepath.Join("Factorio", "factorio.app")
			}

			out = append(out, filepath.Join(lib, "steamapps", "common", name))
		}
	}

	switch runtime.GOOS {
	case "darwin":
		out = append(out, "/Applications/factorio.app", filepath.Join(home, "Applications", "factorio.app"))
	case "windows":
		out = append(out, filepath.Join(os.Getenv("ProgramFiles"), "Factorio"))
	default:
		out = append(out, filepath.Join(home, "factorio"), filepath.Join(home, ".local", "share", "factorio"), "/opt/factorio")
	}

	return dedupe(out)
}

// libraryPath is a library's line in Steam's libraryfolders.vdf.
var libraryPath = regexp.MustCompile(`"path"\s+"([^"]+)"`)

// libraries is every Steam library a Steam install knows of, its own
// first. A game can live in any of them.
func libraries(steam string) []string {
	out := []string{steam}

	b, err := os.ReadFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return out
	}

	for _, m := range libraryPath.FindAllStringSubmatch(string(b), -1) {
		out = append(out, strings.ReplaceAll(m[1], `\\`, `\`))
	}

	return dedupe(out)
}

// userFor is where a copy of the game keeps the player's files: the
// system's place for them, unless the game's config-path.cfg says to keep
// them beside the game, as a copy unpacked from a zip does.
func userFor(root string) string {
	if !systemFolders(root) {
		return root
	}

	home, _ := os.UserHomeDir()

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "factorio")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Factorio")
	default:
		// Steam as a Flatpak keeps the game's files inside its own sandbox.
		flatpak := filepath.Join(home, ".var", "app", "com.valvesoftware.Steam")
		if strings.HasPrefix(root, flatpak+string(filepath.Separator)) {
			return filepath.Join(flatpak, ".factorio")
		}

		return filepath.Join(home, ".factorio")
	}
}

// systemFolders reads use-system-read-write-data-directories out of the
// game's config-path.cfg, which says true unless someone changed it.
func systemFolders(root string) bool {
	cfg := filepath.Join(root, "config-path.cfg")
	if runtime.GOOS == "darwin" {
		cfg = filepath.Join(root, "Contents", "config-path.cfg")
	}

	f, err := os.Open(cfg)
	if err != nil {
		return true
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if ok && strings.TrimSpace(k) == "use-system-read-write-data-directories" {
			return strings.TrimSpace(v) != "false"
		}
	}

	return true
}

// identify asks the game for its version and whether it's the headless
// build. The first line it prints is
// "Version: 2.1.24 (build 87808, linux64, headless)".
func identify(bin string) (version string, headless bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = gameEnv(os.Environ())
	hide(cmd)

	out, err := cmd.Output()
	if err != nil {
		return "", false
	}

	line, _, _ := strings.Cut(string(out), "\n")

	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Version: ")
	if !ok {
		return "", false
	}

	version, build, _ := strings.Cut(rest, " ")

	return version, strings.Contains(build, "headless")
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]

	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}

		seen[s] = true
		out = append(out, s)
	}

	return out
}
