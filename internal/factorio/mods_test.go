package factorio

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func put(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func zipped(t *testing.T, path string, files map[string]string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		_, _ = w.Write([]byte(body))
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	_ = f.Close()
}

func install(t *testing.T) *Install {
	t.Helper()

	root := t.TempDir()
	in := &Install{Data: filepath.Join(root, "data"), User: filepath.Join(root, "user")}

	put(t, filepath.Join(in.Data, "base", "info.json"), `{"name":"base","version":"2.0.77","title":"Base Mod"}`)
	put(t, filepath.Join(in.Data, "core", "info.json"), `{"name":"core","version":"2.0.77"}`)
	put(t, filepath.Join(in.Data, "space-age", "info.json"), `{"name":"space-age","version":"2.0.77","dependencies":["base","quality"]}`)
	put(t, filepath.Join(in.Data, "quality", "info.json"), `{"name":"quality","version":"2.0.77"}`)

	mods := filepath.Join(in.User, "mods")
	if err := os.MkdirAll(mods, 0o755); err != nil {
		t.Fatal(err)
	}

	zipped(t, filepath.Join(mods, "big-mod_1.0.0.zip"), map[string]string{"big-mod_1.0.0/info.json": `{"name":"big-mod","version":"1.0.0"}`})
	zipped(t, filepath.Join(mods, "big-mod_1.10.0.zip"), map[string]string{"big-mod/info.json": `{"name":"big-mod","version":"1.10.0","title":"Big Mod","dependencies":["base >= 2.0","lib-mod >= 0.5","? nice-to-have","! enemy","(?) hidden"]}`})
	zipped(t, filepath.Join(mods, "lib-mod_0.6.1.zip"), map[string]string{"lib-mod_0.6.1/info.json": `{"name":"lib-mod","version":"0.6.1"}`})
	put(t, filepath.Join(mods, "dev-mod", "info.json"), `{"name":"dev-mod","version":"0.0.1","dependencies":["~ big-mod"]}`)
	put(t, filepath.Join(mods, "mod-list.json"), `{"mods":[{"name":"base","enabled":true},{"name":"quality","enabled":false},{"name":"big-mod","enabled":true},{"name":"lib-mod","enabled":true}]}`)

	return in
}

func TestMods(t *testing.T) {
	in := install(t)

	all, err := in.Mods()
	if err != nil {
		t.Fatal(err)
	}

	var got []string

	for _, m := range all {
		on := " "
		if m.Enabled {
			on = "x"
		}

		got = append(got, on+m.Name+"@"+m.Version)
	}
	// Base first, then what ships with the game, then the player's, the
	// newest of each. Space Age isn't named in the list, so it's on;
	// quality is named off.
	want := []string{"xbase@2.0.77", " quality@2.0.77", "xspace-age@2.0.77", "xbig-mod@1.10.0", " dev-mod@0.0.1", "xlib-mod@0.6.1"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}

	big := all[slices.IndexFunc(all, func(m Mod) bool { return m.Name == "big-mod" })]
	if !slices.Equal(big.Requires, []string{"base", "lib-mod"}) || !strings.HasSuffix(big.Path, "big-mod_1.10.0.zip") {
		t.Errorf("big-mod: %+v", big)
	}
}

func TestPick(t *testing.T) {
	all, err := install(t).Mods()
	if err != nil {
		t.Fatal(err)
	}

	names := func(ms []Mod) []string {
		var out []string
		for _, m := range ms {
			out = append(out, m.Name)
		}

		return out
	}

	picked, err := Pick(all, []string{"dev-mod"}, true)
	if err != nil {
		t.Fatal(err)
	}

	if got := names(picked); !slices.Equal(got, []string{"base", "big-mod", "dev-mod", "lib-mod"}) {
		t.Errorf("dev-mod and what it needs: %v", got)
	}

	if picked, _ = Pick(all, []string{"dev-mod"}, false); !slices.Equal(names(picked), []string{"base", "dev-mod"}) {
		t.Errorf("dev-mod alone: %v", names(picked))
	}

	if _, err := Pick(all, []string{"nowhere"}, true); err == nil {
		t.Error("a mod that isn't there was picked")
	}

	if got := names(must(Pick(all, Switched(all), true))); !slices.Equal(got, []string{"base", "quality", "space-age", "big-mod", "lib-mod"}) {
		t.Errorf("as the game has them: %v", got)
	}
}

func must(ms []Mod, err error) []Mod {
	if err != nil {
		panic(err)
	}

	return ms
}

func TestScratch(t *testing.T) {
	in := install(t)
	all, _ := in.Mods()
	put(t, filepath.Join(in.ModsDir(), "mod-settings.dat"), "settings")

	picked := must(Pick(all, []string{"big-mod"}, true))

	s, err := in.NewScratch(t.TempDir(), picked, Mine, "de")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	ini, _ := os.ReadFile(s.config)
	if !strings.Contains(string(ini), "write-data="+s.write) || !strings.Contains(string(ini), "locale=de") {
		t.Errorf("config.ini:\n%s", ini)
	}

	list, _ := os.ReadFile(filepath.Join(s.mods, "mod-list.json"))
	for _, want := range []string{`"name": "big-mod",\n      "enabled": true`, `"name": "space-age",\n      "enabled": false`} {
		if !strings.Contains(string(list), strings.ReplaceAll(want, `\n`, "\n")) {
			t.Errorf("mod-list.json doesn't have %s:\n%s", want, list)
		}
	}

	for _, f := range []string{"big-mod_1.10.0.zip", "lib-mod_0.6.1.zip", "mod-settings.dat"} {
		if _, err := os.Stat(filepath.Join(s.mods, f)); err != nil {
			t.Errorf("%s isn't in the scratch mods: %v", f, err)
		}
	}
	// The older version stays out, so there's no telling which loads.
	if _, err := os.Stat(filepath.Join(s.mods, "big-mod_1.0.0.zip")); err == nil {
		t.Error("the older big-mod came too")
	}
}

func TestRequired(t *testing.T) {
	for dep, want := range map[string]string{
		"base >= 2.0":          "base",
		"~ lib":                "lib",
		"mod-name":             "mod-name",
		"? optional":           "",
		"! incompatible":       "",
		"(?) hidden":           "",
		"  spaced  >  1 ":      "spaced",
		"Flare Stack":          "Flare Stack",
		"Flare Stack >= 4.1.0": "Flare Stack",
		"~ Flow Control":       "Flow Control",
		"? Flare Stack":        "",
	} {
		name, ok := required(dep)
		if want == "" && ok || want != "" && name != want {
			t.Errorf("required(%q) = %q, %v", dep, name, ok)
		}
	}
}
