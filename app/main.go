// Factory Pack makes a mod pack for the factory planner from your own copy
// of Factorio. It's the command-line tool with a window: everything it does
// is in the module above, and this is the window.
package main

import (
	"embed"
	"io/fs"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// version is set by the release build.
var version = "dev"

//go:embed all:frontend
var frontend embed.FS

func main() {
	page, err := fs.Sub(frontend, "frontend")
	if err != nil {
		log.Fatal(err)
	}

	app := application.New(application.Options{
		Name:        "Factory Pack",
		Description: "Makes a mod pack for the factory planner from your own copy of Factorio",
		Services:    []application.Service{application.NewService(newService())},
		Assets:      application.AssetOptions{Handler: application.BundledAssetFileServer(page)},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})

	// The page's own colour from the start, so the window doesn't flash
	// white on its way to dark.
	background := application.NewRGB(0xff, 0xff, 0xff)
	if app.Env.IsDarkMode() {
		background = application.NewRGB(0x16, 0x1c, 0x26)
	}

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Factory Pack",
		Width:            760,
		Height:           940,
		MinWidth:         600,
		MinHeight:        640,
		URL:              "/",
		BackgroundColour: background,
		// On a Mac the page runs up under the traffic lights, which leaves
		// room for them.
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHiddenInset,
			InvisibleTitleBarHeight: 52,
		},
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
