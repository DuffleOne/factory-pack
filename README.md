# Factory Pack

Factory Pack makes a mod pack for the [factory planner](https://factory.duffle.one) out of your own copy of Factorio. It runs the game, mods and settings and all, has it write out its data, and sends that to the planner for a code. You type the code into the planner's game data menu, under "my own mod pack", and plan with your mods.

It comes as an app with a window, and as `factory-pack` for the command line. Both do the same thing.

## What it sends, and what it doesn't

Nothing of yours leaves your machine, and no mods do either. What goes is what the game writes when asked:

- `data-raw-dump.json`: every item, recipe and building in your game, with your mods and their settings applied. The game writes it with `--dump-data`.
- What everything is called, one file per kind of thing, from `--dump-prototype-locale`.
- Where pipes join each crafting machine, recipe by recipe. A small scenario, [`fluidboxes.lua`](internal/factorio/fluidboxes.lua), has the game work that out and write it down.
- A manifest: the game's version, the name and version of each mod loaded, and what you called the pack.
- The pictures the planner asks for, to draw icons and buildings with.

Pictures are the one thing read out of the game's files, so they're held to rules the tool keeps whatever the planner asks for. A picture is a `.png` named by the path a prototype gives it (`__base__/graphics/icons/iron-plate.png`), inside the game's own data folder or inside one of the mods you picked, and its first bytes say PNG. A path that climbs out of its folder, a link, or anything else is refused before a file is opened. So the tool can't be asked for your saves, your settings or your Factorio login, which lives in `player-data.json` and is never read.

The game runs in a folder of its own, made fresh in your temporary folder and deleted afterwards. Its mod folder holds links to the mods you picked and a copy of your `mod-settings.dat`, which is how the game loads your settings. The settings file stays here. Your own mods folder, saves and settings are never written to.

The planner checks what it gets as if anyone could have sent it: an export is unpacked with limits on every file, and a picture is decoded and kept as the pixels alone.

To see everything that went, set "keep a copy of everything sent in" (`-keep` on the command line). To look before sending anything, `factory-pack -no-send -keep somewhere` makes the export and stops.

## Getting it

From [the releases](https://github.com/DuffleOne/factory-pack/releases/latest):

- **Windows**: `FactoryPack-windows.zip`. It isn't signed, so Windows says it protected your PC the first time. Click "More info", then "Run anyway".
- **macOS**: `FactoryPack-macos.zip`, signed and notarised, for Apple silicon and Intel.
- **Linux**: `FactoryPack-linux.tar.gz`. The window needs GTK 3 and WebKitGTK 4.1, which most desktops have (`libwebkit2gtk-4.1-0` on Debian and Ubuntu, `webkit2gtk4.1` on Fedora).
- **Steam Deck and Bazzite**: SteamOS has no WebKitGTK, so use the command-line version, `factory-pack-linux-amd64.tar.gz`, from the desktop's terminal.

Each release lists the SHA-256 of every file in `SHA256SUMS`, and carries a build attestation: `gh attestation verify <file> --repo DuffleOne/factory-pack` checks a download was built here, from this source.

## Using it

Open it. It finds Factorio by itself in the usual places, Steam libraries included, and ticks the mods your game has switched on. Untick what you don't want, tick what you do, and press "make my pack". The game runs three times in the background, a minute or two each with a lot of mods, then the pack goes up and the code comes back.

The same game, mods and settings always get the same code, so running it again after a pack has gone brings back the same one. A pack lasts 48 hours after it was last used, and opening it or planning with it counts as using it.

To make a pack of the game a multiplayer server runs, ask whoever runs it for its `mod-list.json` and `mod-settings.dat`. "as a mod-list.json has them" ticks the mods its list has on, and "more" takes the settings file. Every mod on the list has to be in your own mods folder, which it will be if you play there.

"more" has the rest: a name for the pack, which mod settings to load (yours, every mod's defaults, or a `mod-settings.dat` from a server), the language things are named in, another planner to send to, where to keep a copy of what's sent, and where the game runs. Whatever's set is kept for next time, in `factory-pack/config.json` in your system's settings folder.

### On the command line

```
factory-pack -list
factory-pack
factory-pack -mods Krastorio2-spaced-out -settings defaults -name "K2 Spaced Out"
factory-pack -mod-list ~/server/mod-list.json -settings ~/server/mod-settings.dat -name "our server"
```

`factory-pack -h` lists every flag. Each one matches a setting in the app, and `-save` keeps them in the config file. If the game isn't found, `-game` says where its executable is, or `FACTORIO_PATH` does.

## How it fits together

- `internal/factorio` finds the game, lists its mods and runs it in a scratch folder.
- `internal/pack` makes the export, reads pictures under the rules above, and talks to the planner.
- `cmd/factory-pack` is the command line, with no dependencies past Go's own.
- `app` is the window: a [Wails](https://wails.io) app in a module of its own, with a page of plain HTML and JavaScript and no build step.

The planner takes a pack in three steps. `POST /api/packs` takes the export, a gzipped tar, and answers with the pack's code, a token, and the pictures it wants. `POST /api/packs/{code}/pictures` takes them in batches, as plain tars, with the token. `POST /api/packs/{code}/build` starts the build, and `GET /api/packs/{code}` says how it stands.

## Building it

The command line, from anywhere, for anywhere:

```
go build ./cmd/factory-pack
GOOS=windows go build ./cmd/factory-pack
```

The app has to be built on the system it's for, Windows aside:

```
cd app
go build -tags gtk3,production .                                    # Linux, with libgtk-3-dev and libwebkit2gtk-4.1-dev
go build -tags production .                                         # macOS
GOOS=windows go build -tags production -ldflags "-H windowsgui" .   # Windows, from anywhere
```

`go test ./...` at the top tests the tool. The workflows in `.github/workflows` build and release it, and sign and notarise the Mac build when the `MACOS_*` and `APPLE_*` secrets are set.

## Licence

MIT. See [LICENSE](LICENSE).
