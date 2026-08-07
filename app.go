package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	appruntime "superscience/internal/runtime"
)

type App struct {
	ctx      context.Context
	cancel   context.CancelFunc
	instance *appruntime.Instance
	mu       sync.RWMutex
	startErr error
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx, a.cancel = context.WithCancel(ctx)
	root := os.Getenv("SUPERSCIENCE_PROJECT_ROOT")
	if root == "" {
		for _, argument := range os.Args[1:] {
			if info, err := os.Stat(argument); err == nil && info.IsDir() {
				root = argument
				break
			}
		}
	}
	if root == "" {
		root, _ = os.Getwd()
	}
	root, _ = filepath.Abs(root)
	instance, err := appruntime.Start(a.ctx, appruntime.Options{Port: 0, ProjectRoot: root})
	a.mu.Lock()
	a.instance = instance
	a.startErr = err
	a.mu.Unlock()
}

func (a *App) shutdown(ctx context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *App) ServerURL() (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.startErr != nil {
		return "", a.startErr
	}
	if a.instance == nil {
		return "", errors.New("local Superscience server has not started")
	}
	return a.instance.URL, nil
}

func (a *App) OpenProjectFolder() (string, error) {
	if a.ctx == nil {
		return "", errors.New("desktop application is not ready")
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Open Superscience Project"})
}

func (a *App) chooseProjectFromMenu() {
	path, err := a.OpenProjectFolder()
	if err != nil {
		runtime.EventsEmit(a.ctx, "desktop:error", err.Error())
		return
	}
	if path != "" {
		runtime.EventsEmit(a.ctx, "desktop:open-project", path)
	}
}

func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}
