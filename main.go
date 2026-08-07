package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	appMenu := menu.NewMenu()
	fileMenu := appMenu.AddSubmenu("File")
	fileMenu.AddText("Open Project...", keys.CmdOrCtrl("o"), func(*menu.CallbackData) { app.chooseProjectFromMenu() })
	fileMenu.AddSeparator()
	fileMenu.AddText("Quit Superscience", keys.CmdOrCtrl("q"), func(*menu.CallbackData) {
		if app.ctx != nil {
			runtime.Quit(app.ctx)
		}
	})
	err := wails.Run(&options.App{
		Title:     "Superscience",
		Width:     1440,
		Height:    900,
		MinWidth:  1100,
		MinHeight: 700,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Menu:       appMenu,
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "cc.superscience.workbench",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				app.showWindow()
				if len(data.Args) > 1 {
					runtime.EventsEmit(app.ctx, "desktop:open-project", data.Args[len(data.Args)-1])
				}
			},
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
