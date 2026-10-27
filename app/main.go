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

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Factory Pack",
		Width:     780,
		Height:    820,
		MinWidth:  560,
		MinHeight: 560,
		URL:       "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
