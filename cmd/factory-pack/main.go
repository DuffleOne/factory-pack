// factory-pack makes a mod pack for the factory planner from your own copy
// of Factorio, and sends it for a code to type in there. This is the
// command-line version; Factory Pack is the same with a window.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/duffleone/factory-pack/internal/config"
	"github.com/duffleone/factory-pack/internal/factorio"
	"github.com/duffleone/factory-pack/internal/pack"
)

// version is set by the release build.
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "factory-pack:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("factory-pack", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `factory-pack makes a mod pack for the factory planner out of your own copy
of Factorio. It runs the game in a folder of its own to have it write out
its data, mods and settings applied, and sends that, and the pictures the
planner asks for, for a code to type in there.

usage: factory-pack [flags]

`)
		fs.PrintDefaults()
	}

	path, _ := config.Path()

	cfgPath := fs.String("config", path, "the config file to read, and to write with -save")
	list := fs.Bool("list", false, "say which game it finds and what mods it has, and stop")
	server := fs.String("server", "", "the planner to send to (default "+pack.DefaultServer+")")
	binary := fs.String("game", "", "the game's executable, if it isn't found by itself")
	data := fs.String("data", "", "the game's data folder, the one holding base and core")
	user := fs.String("user", "", "the folder holding your mods and saves")
	mods := fs.String("mods", "", "the mods to load, as mod,mod (default: what the game has switched on)")
	modList := fs.String("mod-list", "", "load the mods a mod-list.json has switched on, a server's say, in place of -mods")
	exact := fs.Bool("exact", false, "load only the mods named, without what they need")
	settings := fs.String("settings", "", "mod settings: mine, defaults, or a mod-settings.dat (default mine)")
	locale := fs.String("locale", "", "the language to name things in, as en, de or pt-BR (default the game's)")
	name := fs.String("name", "", "what to call the pack in the planner")
	keep := fs.String("keep", "", "a folder to keep a copy of everything sent in")
	noSend := fs.Bool("no-send", false, "make the export and stop, with -keep to look at it")
	work := fs.String("work", "", "where the game runs (default the system's temporary folder)")
	timeout := fs.String("timeout", "", "the longest one run of the game may take (default 15m)")
	save := fs.Bool("save", false, "remember these flags in the config file for next time")
	showVersion := fs.Bool("version", false, "say which version this is, and stop")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	if *showVersion {
		fmt.Println("factory-pack", version)
		return nil
	}

	c, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", *cfgPath, err)
	}

	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	str := func(flagName string, from string, to *string) {
		if set[flagName] {
			*to = from
		}
	}

	str("server", *server, &c.Server)
	str("game", *binary, &c.Binary)
	str("data", *data, &c.Data)
	str("user", *user, &c.User)
	str("settings", *settings, &c.Settings)
	str("locale", *locale, &c.Locale)
	str("name", *name, &c.Name)
	str("keep", *keep, &c.Keep)
	str("work", *work, &c.Work)
	str("timeout", *timeout, &c.Timeout)

	if set["mods"] {
		c.Mods = nil

		for _, m := range strings.Split(*mods, ",") {
			if m = strings.TrimSpace(m); m != "" {
				c.Mods = append(c.Mods, m)
			}
		}
	}

	if set["mod-list"] {
		if set["mods"] {
			return errors.New("-mods and -mod-list each say what to load: pick one")
		}

		names, err := factorio.ModList(*modList)
		if err != nil {
			return err
		}

		c.Mods = names
	}

	if set["exact"] {
		c.Exact = *exact
	}

	if *save {
		if err := c.Save(*cfgPath); err != nil {
			return err
		}

		fmt.Println("saved to", *cfgPath)
	}

	if *list {
		return show(c)
	}

	if *noSend && c.Keep == "" {
		return errors.New("-no-send keeps nothing without -keep to put it in")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	start := time.Now()
	job := &pack.Job{Config: c, Tool: "factory-pack " + version, NoSend: *noSend}

	done, err := job.Run(ctx, func(s pack.Step) {
		if s.Of > 0 {
			fmt.Printf("%s (%d of %d)\n", s.Stage, s.Done, s.Of)
			return
		}

		fmt.Println(s.Stage)
	})
	if err != nil {
		return err
	}

	fmt.Printf("\nthe game's data came to %s packed, in %d files", size(done.Export.Size), len(done.Export.Files))

	if done.Sent == nil {
		fmt.Printf(". kept in %s, not sent.\n", done.Kept)
		return nil
	}

	fmt.Printf(", and %d pictures went with it", len(done.Sent.Pictures))

	if n := len(done.Sent.Missing); n > 0 {
		fmt.Printf(" (%d asked for couldn't be found, so those go without)", n)
	}

	fmt.Printf(". took %s.\n", time.Since(start).Round(time.Second))

	if done.Kept != "" {
		fmt.Println("a copy of everything sent is in", done.Kept)
	}

	fmt.Printf("\nyour pack's code:\n\n    %s\n\n", pack.Pretty(done.Sent.Code))

	fmt.Printf("open %s, or pick \"my own mod pack\" in the game data menu and type it in.\n", pack.Link(c.Server, done.Sent.Code))

	fmt.Println("it's kept for 48 hours after it was last used. the same mods and settings always get the same code.")

	return nil
}

// show says what the tool finds: the game, and its mods, ticked where
// they'd be loaded.
func show(c *config.Config) error {
	in, all, err := pack.Mods(c)
	if err != nil {
		return err
	}

	kind := ""
	if in.Headless {
		kind = ", headless"
	}

	fmt.Printf("game:  %s (%s%s)\ndata:  %s\nmods:  %s\n\n", in.Binary, in.Version, kind, in.Data, in.ModsDir())

	names := c.Mods
	if len(names) == 0 {
		names = factorio.Switched(all)
	}

	picked, err := factorio.Pick(all, names, !c.Exact)
	if err != nil {
		// Still tick what was asked for, so the list says what's wrong
		// with it rather than nothing at all.
		fmt.Printf("those mods won't load together: %v\n\n", err)
		picked, _ = factorio.Pick(all, names, false)
	}

	on := map[string]bool{}
	for _, m := range picked {
		on[m.Name] = true
	}

	for _, m := range all {
		tick := " "
		if on[m.Name] {
			tick = "x"
		}

		title := ""
		if m.Title != "" && m.Title != m.Name {
			title = "  " + m.Title
		}

		fmt.Printf("[%s] %s %s%s\n", tick, m.Name, m.Version, title)
	}

	return nil
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}
