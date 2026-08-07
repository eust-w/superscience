package buildinfo

import "strings"

// These values are replaced by release builds through -ldflags -X.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func DisplayVersion() string {
	version := strings.TrimSpace(Version)
	if version == "" {
		return "dev"
	}
	return version
}
