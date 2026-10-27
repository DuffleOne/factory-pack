#!/usr/bin/env bash
# Makes dist/FactoryPack-Linux.AppImage: the app with GTK 3 and WebKitGTK
# inside, for desktops without them (SteamOS, Bazzite). Run it from the repo
# root on Ubuntu 22.04, whose glibc is the oldest it starts on. VERSION sets
# the version the app reports.
set -euo pipefail

VERSION="${VERSION:-dev}"
id=one.duffle.factory-pack

sudo=; [ "$(id -u)" = 0 ] || sudo=sudo
$sudo apt-get update -qq
$sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends \
  build-essential pkg-config git ca-certificates wget file dpkg-dev \
  libgtk-3-dev libwebkit2gtk-4.1-dev librsvg2-common >/dev/null

if ! command -v go >/dev/null; then
  wget -qO- https://go.dev/dl/go1.27.1.linux-amd64.tar.gz | tar -xz -C "${RUNNER_TEMP:-/tmp}"
  export PATH="${RUNNER_TEMP:-/tmp}/go/bin:$PATH"
fi

work="$PWD/out/appimage"
rm -rf "$work"
mkdir -p "$work" dist
(cd app && go build -trimpath -tags gtk3,production -ldflags "-s -w -X main.version=$VERSION" -o "$work/factory-pack-app" .)
cp app/build/linux/factory-pack.png "$work/$id.png"
cat > "$work/$id.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Factory Pack
Comment=Makes a mod pack for the factory planner from your own copy of Factorio
Exec=factory-pack-app
Icon=$id
Categories=Utility;
Terminal=false
StartupWMClass=factory-pack-app
EOF

# Wails' own generator: linuxdeploy with its GTK plugin, plus WebKit's helper
# processes, which it moves beside the libraries and points WebKit at.
wails=$(cd app && go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3)
export APPIMAGE_EXTRACT_AND_RUN=1
go run -tags gtk3 "github.com/wailsapp/wails/v3/cmd/wails3@$wails" generate appimage \
  -binary "$work/factory-pack-app" -icon "$work/$id.png" -desktopfile "$work/$id.desktop" \
  -outputdir "$work" -builddir "$work/build"

# Two fixes to what it made, then pack it again. Wails' AppRun never sources
# the GTK plugin's hook, so outside Debian and Ubuntu GTK finds none of the
# modules bundled for it, the SVG icon loader among them. And its
# LD_LIBRARY_PATH reaches everything the app starts, so xdg-open's helpers
# and the browser get the bundled libraries over their own (Fedora's gio
# won't start with them). So AppRun keeps the environment it was given, and
# usr/bin/xdg-open puts it back.
appdir="$work/build/factory-pack-app-x86_64.AppDir"
mv "$appdir/AppRun" "$appdir/AppRun.wrapped"
cat > "$appdir/AppRun" <<'EOF'
#!/usr/bin/env bash
here="$(dirname "$(readlink -f "$0")")"
export APPDIR="${APPDIR:-$here}"
for v in PATH LD_LIBRARY_PATH XDG_DATA_DIRS GSETTINGS_SCHEMA_DIR GDK_BACKEND GTK_THEME \
  GTK_DATA_PREFIX GTK_EXE_PREFIX GTK_PATH GTK_IM_MODULE_FILE GDK_PIXBUF_MODULE_FILE \
  GI_TYPELIB_PATH WEBKIT_INJECTED_BUNDLE_PATH; do
  if [ -n "${!v+x}" ]; then export "FP_ORIG_$v=${!v}"; else export "FP_UNSET_$v=1"; fi
done
for hook in "$here"/apprun-hooks/*.sh; do . "$hook"; done
exec "$here/AppRun.wrapped" "$@"
EOF
cat > "$appdir/usr/bin/xdg-open" <<'EOF'
#!/usr/bin/env bash
for v in $(compgen -v FP_ORIG_); do export "${v#FP_ORIG_}=${!v}"; unset "$v"; done
for v in $(compgen -v FP_UNSET_); do unset "${v#FP_UNSET_}" "$v"; done
exec xdg-open "$@"
EOF
chmod +x "$appdir/AppRun" "$appdir/usr/bin/xdg-open"
rm "$work/factory-pack-app-x86_64.AppImage"
(cd "$work/build" && OUTPUT=FactoryPack-Linux.AppImage ./linuxdeploy-x86_64.AppImage --appdir "$appdir" --output appimage >/dev/null)
mv "$work/build/FactoryPack-Linux.AppImage" dist/
ls -l dist/FactoryPack-Linux.AppImage
