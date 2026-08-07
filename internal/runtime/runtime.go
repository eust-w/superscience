package runtime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"superscience/internal/server"
	"superscience/internal/store"
)

type Instance struct {
	URL    string
	Server *http.Server
	Store  *store.Store

	closeOnce sync.Once
	closeErr  error
}

func (i *Instance) Close(ctx context.Context) error {
	i.closeOnce.Do(func() {
		serverErr := i.Server.Shutdown(ctx)
		storeErr := i.Store.Close()
		if serverErr != nil {
			i.closeErr = serverErr
		} else {
			i.closeErr = storeErr
		}
	})
	return i.closeErr
}

type Options struct {
	Port        int
	ProjectRoot string
}

func DataDir() (string, error) {
	if dir := os.Getenv("SUPERSCIENCE_DATA_DIR"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "superscience"), nil
}

func Start(ctx context.Context, options Options) (*Instance, error) {
	dataDir, err := DataDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	db, err := store.Open(ctx, filepath.Join(dataDir, "superscience.db"))
	if err != nil {
		return nil, err
	}
	app := server.New(server.Options{Store: db, ProjectRoot: options.ProjectRoot, StaticDir: discoverStaticDir()})
	addr := fmt.Sprintf("127.0.0.1:%d", options.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	httpServer := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "superscience server error: %v\n", err)
		}
	}()
	instance := &Instance{
		URL:    "http://" + ln.Addr().String(),
		Server: httpServer,
		Store:  db,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = instance.Close(shutdownCtx)
	}()
	return instance, nil
}

func discoverStaticDir() string {
	if dir := os.Getenv("SUPERSCIENCE_STATIC_DIR"); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
			return dir
		}
	}
	candidates := []string{
		filepath.Join("frontend", "dist"),
		"dist",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil {
			return candidate
		}
	}
	return ""
}

func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
