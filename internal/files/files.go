package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var ErrOutsideRoot = errors.New("path escapes project root")

type Node struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Size      int64  `json:"size"`
	Modified  int64  `json:"modified"`
	Extension string `json:"extension,omitempty"`
}

func ResolveProjectPath(root, requested string) (string, error) {
	if root == "" {
		return "", errors.New("project root is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		rootReal = rootAbs
	}

	requested = strings.TrimPrefix(requested, string(filepath.Separator))
	candidate := filepath.Clean(filepath.Join(rootReal, requested))
	rel, err := filepath.Rel(rootReal, candidate)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", ErrOutsideRoot
	}

	if info, err := os.Lstat(candidate); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return "", err
			}
			rel, err := filepath.Rel(rootReal, real)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
				return "", ErrOutsideRoot
			}
			return real, nil
		}
	}
	return candidate, nil
}

func ReadText(root, path string) (string, error) {
	resolved, err := ResolveProjectPath(root, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func WriteText(root, path, content string) error {
	resolved, err := ResolveProjectPath(root, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return err
	}
	return os.WriteFile(resolved, []byte(content), 0o644)
}

func List(root, path string) ([]Node, error) {
	resolved, err := ResolveProjectPath(root, path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		kind := "file"
		if entry.IsDir() {
			kind = "directory"
		}
		rel, _ := filepath.Rel(root, filepath.Join(resolved, entry.Name()))
		nodes = append(nodes, Node{
			Name:      entry.Name(),
			Path:      rel,
			Kind:      kind,
			Size:      info.Size(),
			Modified:  info.ModTime().UnixMilli(),
			Extension: strings.TrimPrefix(filepath.Ext(entry.Name()), "."),
		})
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Kind != nodes[j].Kind {
			return nodes[i].Kind == "directory"
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
	return nodes, nil
}

func Search(root, query string, limit int) ([]Node, error) {
	if limit <= 0 {
		limit = 50
	}
	matches := make([]Node, 0)
	query = strings.ToLower(query)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(matches) >= limit {
			return nil
		}
		name := d.Name()
		if name == ".git" || name == "node_modules" || name == "reference" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.Contains(strings.ToLower(name), query) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		kind := "file"
		if d.IsDir() {
			kind = "directory"
		}
		rel, _ := filepath.Rel(root, path)
		matches = append(matches, Node{Name: name, Path: rel, Kind: kind, Size: info.Size(), Modified: info.ModTime().UnixMilli(), Extension: strings.TrimPrefix(filepath.Ext(name), ".")})
		return nil
	})
	return matches, err
}
