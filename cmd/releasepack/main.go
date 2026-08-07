package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	releasepkg "superscience/internal/release"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("releasepack", flag.ContinueOnError)
	fs.SetOutput(stderr)
	version := fs.String("version", "", "semantic release version")
	goos := fs.String("goos", "", "target operating system")
	goarch := fs.String("goarch", "", "target architecture")
	binary := fs.String("binary", "", "compiled Superscience CLI binary")
	output := fs.String("output", "dist", "release output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected releasepack arguments: %s", strings.Join(fs.Args(), " "))
	}
	for _, required := range []struct {
		name  string
		value string
	}{
		{name: "version", value: *version},
		{name: "goos", value: *goos},
		{name: "goarch", value: *goarch},
		{name: "binary", value: *binary},
	} {
		if strings.TrimSpace(required.value) == "" {
			return errors.New("release " + required.name + " is required")
		}
	}
	result, err := releasepkg.PackageBinary(releasepkg.PackageOptions{
		Version: *version, GOOS: *goos, GOARCH: *goarch, BinaryPath: *binary, OutputDir: *output,
	})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
