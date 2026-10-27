package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/duffleone/factory-pack/internal/config"
	"github.com/duffleone/factory-pack/internal/factorio"
	"github.com/duffleone/factory-pack/internal/pack"
)

// Service is what the window asks of Go. Each exported method is callable
// from the page as "main.Service.Name".
type Service struct {
	path string

	mu   sync.Mutex
	stop context.CancelFunc
}

func newService() *Service {
	path, _ := config.Path()
	return &Service{path: path}
}

// State is what the window shows: the settings, the game they find, and
// its mods, or why there's no game to show.
type State struct {
	Config  *config.Config    `json:"config"`
	Game    *factorio.Install `json:"game,omitempty"`
	Mods    []factorio.Mod    `json:"mods"`
	Problem string            `json:"problem,omitempty"`
	// Picked is the mods that would load: those named, or what the game has
	// switched on, with what they need.
	Picked  []string `json:"picked"`
	Version string   `json:"version"`
	// ConfigPath is where the settings are kept.
	ConfigPath string `json:"configPath"`
	// OS is the system, as Go names it, and Home the player's home folder,
	// for showing paths short.
	OS   string `json:"os"`
	Home string `json:"home"`
	// Where is the game as the player knows it: on a Mac, the app rather
	// than the program inside it.
	Where string `json:"where,omitempty"`
}

// Load reads the saved settings and looks for the game with them.
func (s *Service) Load() State {
	c, err := config.Load(s.path)
	if err != nil {
		c = &config.Config{}
	}

	return s.Look(*c)
}

// Look finds the game and its mods with the settings given, as they stand
// in the window, without saving them.
func (s *Service) Look(c config.Config) State {
	home, _ := os.UserHomeDir()
	st := State{Config: &c, Mods: []factorio.Mod{}, Picked: []string{}, Version: version, ConfigPath: s.path, OS: runtime.GOOS, Home: home}

	in, all, err := pack.Mods(&c)
	if err != nil {
		st.Problem = err.Error()
		return st
	}

	st.Game, st.Mods = in, all

	st.Where = in.Binary
	if app, _, ok := strings.Cut(in.Binary, ".app/Contents/MacOS/"); ok {
		st.Where = app + ".app"
	}

	names := c.Mods
	if len(names) == 0 {
		names = factorio.Switched(all)
	}

	picked, err := factorio.Pick(all, names, !c.Exact)
	if err != nil {
		st.Problem = err.Error()
		picked, _ = factorio.Pick(all, names, false)
	}

	for _, m := range picked {
		st.Picked = append(st.Picked, m.Name)
	}

	return st
}

// Save keeps the settings for next time.
func (s *Service) Save(c config.Config) error { return c.Save(s.path) }

// Browse asks for a file or a folder: "game" is the executable, "settings"
// a mod-settings.dat, and anything else a folder. It gives back what was
// picked, or nothing.
func (s *Service) Browse(what, from string) (string, error) {
	d := application.Get().Dialog.OpenFile().SetTitle(title(what))
	if from != "" {
		d = d.SetDirectory(from)
	}

	switch what {
	case "game", "settings", "modlist":
		d = d.CanChooseFiles(true).CanChooseDirectories(false)

		switch what {
		case "settings":
			d = d.AddFilter("mod settings", "*.dat")
		case "modlist":
			d = d.AddFilter("mod list", "*.json")
		}
	default:
		d = d.CanChooseFiles(false).CanChooseDirectories(true)
	}

	return d.PromptForSingleSelection()
}

func title(what string) string {
	switch what {
	case "game":
		return "Where's Factorio's executable?"
	case "data":
		return "Factorio's data folder, the one with base and core in it"
	case "user":
		return "The folder with your mods and saves in it"
	case "settings":
		return "A mod-settings.dat to load the mods with"
	case "modlist":
		return "A mod-list.json, a server's say, to tick the mods it has on"
	case "keep":
		return "Where to keep a copy of everything sent"
	default:
		return "Pick a folder"
	}
}

// ModList is the mods a mod-list.json has switched on.
func (s *Service) ModList(path string) ([]string, error) { return factorio.ModList(path) }

// Done is what making a pack came to, for the window.
type Done struct {
	Code   string `json:"code"`
	Pretty string `json:"pretty"`
	Link   string `json:"link"`
	Name   string `json:"name"`
	// Expires is when the pack goes unless it's used, RFC 3339.
	Expires  string `json:"expires"`
	Size     int64  `json:"size"`
	Files    int    `json:"files"`
	Pictures int    `json:"pictures"`
	Missing  int    `json:"missing"`
	Kept     string `json:"kept,omitempty"`
}

// Make saves the settings, then makes the pack and sends it, saying how
// it's going as "progress" events. Stop ends it early.
func (s *Service) Make(c config.Config) (*Done, error) {
	if err := c.Save(s.path); err != nil {
		return nil, fmt.Errorf("saving the settings: %w", err)
	}

	ctx, stop := context.WithCancel(context.Background())

	s.mu.Lock()
	if s.stop != nil {
		s.mu.Unlock()
		stop()

		return nil, errors.New("a pack is being made already")
	}

	s.stop = stop
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.stop = nil
		s.mu.Unlock()
		stop()
	}()

	job := &pack.Job{Config: &c, Tool: "factory-pack " + version}

	done, err := job.Run(ctx, func(st pack.Step) {
		application.Get().Event.Emit("progress", st)
	})

	switch {
	case ctx.Err() != nil:
		return nil, errors.New("stopped")
	case err != nil:
		return nil, err
	}

	sent := done.Sent

	return &Done{
		Code:     sent.Code,
		Pretty:   pack.Pretty(sent.Code),
		Link:     pack.Link(c.Server, sent.Code),
		Name:     sent.Name,
		Expires:  sent.Expires,
		Size:     done.Export.Size,
		Files:    len(done.Export.Files),
		Pictures: len(sent.Pictures),
		Missing:  len(sent.Missing),
		Kept:     done.Kept,
	}, nil
}

// Stop stops a pack being made.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stop != nil {
		s.stop()
	}
}

// Open opens a link in the browser.
func (s *Service) Open(url string) error {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return errors.New("that isn't a web address")
	}

	return application.Get().Browser.OpenURL(url)
}

// Copy puts text on the clipboard.
func (s *Service) Copy(text string) bool { return application.Get().Clipboard.SetText(text) }
