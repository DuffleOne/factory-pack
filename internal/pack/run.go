package pack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/duffleone/factory-pack/internal/config"
	"github.com/duffleone/factory-pack/internal/factorio"
)

// Job is one pack to make, with everything said about it.
type Job struct {
	Config *config.Config
	// Tool is what's making it, as "factory-pack 1.0.0".
	Tool string
	// NoSend stops once the export is made.
	NoSend bool
}

// Done is what a job came to.
type Done struct {
	Game   *factorio.Install `json:"game"`
	Export *Export           `json:"export"`
	// Sent is nil for a job that wasn't sent.
	Sent *Sent `json:"sent,omitempty"`
	// Kept is where the copy of everything went, when one was kept.
	Kept string `json:"kept,omitempty"`
}

// Mods finds the game and what mods it has, as a job would load them.
func Mods(c *config.Config) (*factorio.Install, []factorio.Mod, error) {
	in, err := factorio.Find(factorio.Paths{Binary: c.Binary, Data: c.Data, User: c.User})
	if err != nil {
		return nil, nil, err
	}

	all, err := in.Mods()
	if err != nil {
		return nil, nil, fmt.Errorf("reading the mods: %w", err)
	}

	return in, all, nil
}

// Run makes the pack: finds the game, picks the mods, has the game write
// out its data, and sends it, unless the job says not to.
func (j *Job) Run(ctx context.Context, progress func(Step)) (*Done, error) {
	c := j.Config
	if progress == nil {
		progress = func(Step) {}
	}

	in, all, err := Mods(c)
	if err != nil {
		return nil, err
	}

	if in.Headless {
		// It has no pictures, and the pack would have no art. Still worth
		// making, but worth saying.
		progress(Step{Stage: "this is the headless game, with no pictures: the pack will have none"})
	}

	names := c.Mods
	if len(names) == 0 {
		names = factorio.Switched(all)
	}

	mods, err := factorio.Pick(all, names, !c.Exact)
	if err != nil {
		return nil, err
	}

	keep := c.Keep

	out := filepath.Join(keep, "export.tar.gz")
	if keep == "" {
		f, err := os.CreateTemp(c.Work, "factory-pack-*.tar.gz")
		if err != nil {
			return nil, err
		}

		out = f.Name()
		_ = f.Close()

		defer func() { _ = os.Remove(out) }()
	} else if err := os.MkdirAll(keep, 0o755); err != nil {
		return nil, err
	}

	run, cancel := context.WithTimeout(ctx, 3*c.RunTimeout())
	defer cancel()

	exp, err := Make(run, in, Options{
		Name:     strings.TrimSpace(c.Name),
		Mods:     mods,
		Settings: settings(c.Settings),
		Locale:   c.Locale,
		Work:     c.Work,
		Out:      out,
		Tool:     j.Tool,
	}, progress)
	if err != nil {
		return nil, err
	}

	done := &Done{Game: in, Export: exp, Kept: keep}
	if j.NoSend {
		return done, nil
	}

	pics := NewPictures(in, mods)
	defer pics.Close()

	var save func(string, []byte) error
	if keep != "" {
		save = func(name string, b []byte) error {
			dst := filepath.Join(keep, "pictures", filepath.FromSlash(strings.ReplaceAll(name, "__", "")))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}

			return os.WriteFile(dst, b, 0o644)
		}
	}

	client := &Client{Server: c.Server, Agent: j.Tool}

	sent, err := client.Send(ctx, out, pics, progress, save)
	if err != nil {
		return nil, err
	}

	done.Sent = sent

	return done, nil
}

func settings(s string) factorio.Settings {
	switch s {
	case "", "mine":
		return factorio.Mine
	case "defaults":
		return factorio.Defaults
	default:
		return factorio.Settings(s)
	}
}

// Link opens the planner at server with a pack in it.
func Link(server, code string) string {
	base := strings.TrimRight(server, "/")
	if base == "" {
		base = DefaultServer
	}

	return base + "/?pack=" + code
}

// Pretty is a code as people read it, in two fours: "ABCD-2345".
func Pretty(code string) string {
	if len(code) != 8 {
		return code
	}

	return code[:4] + "-" + code[4:]
}
