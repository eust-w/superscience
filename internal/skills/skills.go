package skills

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"superscience/internal/store"
)

func Bundled() []store.SkillRecord {
	return []store.SkillRecord{
		{ID: "research-protocol", Name: "research-protocol", Description: "Structure literature search, evidence extraction, provenance, and final synthesis.", Source: "bundled", Enabled: true},
		{ID: "biology-workbench", Name: "biology-workbench", Description: "Use protein, compound, gene, and pathway databases with molecule/protein artifacts.", Source: "bundled", Enabled: true},
		{ID: "physics-review", Name: "physics-review", Description: "Review equations, arXiv references, plots, and reproducibility notes.", Source: "bundled", Enabled: true},
		{ID: "ml-experiment", Name: "ml-experiment", Description: "Plan ML experiments, inspect code, and capture notebook outputs.", Source: "bundled", Enabled: true},
		{ID: "mcp-tool-author", Name: "mcp-tool-author", Description: "Describe and safely expose external MCP tools through Superscience permissions.", Source: "bundled", Enabled: true},
		{ID: "scientific-rendering", Name: "scientific-rendering", Description: "Choose artifact renderer types for molecules, proteins, genomes, plots, PDF, and LaTeX.", Source: "bundled", Enabled: true},
		{ID: "share-review", Name: "share-review", Description: "Prepare readonly share bundles with artifact, provenance, and audit summaries.", Source: "bundled", Enabled: true},
	}
}

type Loaded struct {
	store.SkillRecord
	Content string `json:"content"`
}

type EntryManifest struct {
	Namespace string   `json:"namespace"`
	Entries   []string `json:"entries"`
	Path      string   `json:"path"`
}

func Merge(bundled []store.SkillRecord, persisted []store.SkillRecord) []store.SkillRecord {
	seen := map[string]store.SkillRecord{}
	for _, skill := range bundled {
		seen[skill.Name] = skill
	}
	for _, skill := range persisted {
		seen[skill.Name] = skill
	}
	result := make([]store.SkillRecord, 0, len(seen))
	for _, skill := range seen {
		result = append(result, skill)
	}
	return result
}

func LoadFromDir(root string) ([]store.SkillRecord, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}
	var result []store.SkillRecord
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" {
			return nil
		}
		skill, err := ParseFile(path)
		if err != nil {
			return err
		}
		result = append(result, skill)
		return nil
	})
	return result, err
}

func LoadProject(projectRoot string) ([]store.SkillRecord, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return nil, nil
	}
	roots := []string{
		filepath.Join(projectRoot, ".superscience", "skills"),
		filepath.Join(projectRoot, ".superscience", "skill"),
	}
	var result []store.SkillRecord
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			continue
		}
		loaded, err := LoadFromDir(root)
		if err != nil {
			return nil, err
		}
		for _, skill := range loaded {
			skill.Source = "project"
			result = append(result, skill)
		}
	}
	return result, nil
}

func UserDataDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("SUPERSCIENCE_DATA_DIR")); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "superscience"), nil
}

func LoadUser() ([]store.SkillRecord, error) {
	dataDir, err := UserDataDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(dataDir, "skills")
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	loaded, err := LoadFromDir(root)
	if err != nil {
		return nil, err
	}
	for i := range loaded {
		loaded[i].Source = "user"
	}
	return applyEntryManifest(root, loaded)
}

func LoadInstalled() ([]store.SkillRecord, error) {
	dataDir, err := UserDataDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(dataDir, "installed-skills")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	result := make([]store.SkillRecord, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !validNamespace(entry.Name()) {
			continue
		}
		namespaceRoot := filepath.Join(root, entry.Name())
		loaded, err := LoadFromDir(namespaceRoot)
		if err != nil {
			return nil, err
		}
		loaded, err = applyEntryManifest(namespaceRoot, loaded)
		if err != nil {
			return nil, err
		}
		for index := range loaded {
			loaded[index].Source = "installed:" + entry.Name()
		}
		result = append(result, loaded...)
	}
	return result, nil
}

func IsEntry(skill store.SkillRecord) bool {
	return skill.Entry == nil || *skill.Entry
}

func SetEntries(namespace string, entries []string) (EntryManifest, error) {
	namespace = strings.TrimSpace(namespace)
	if !validNamespace(namespace) {
		return EntryManifest{}, errors.New("skill namespace must use letters, numbers, underscores, or hyphens")
	}
	root, err := namespaceRoot(namespace)
	if err != nil {
		return EntryManifest{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return EntryManifest{}, fmt.Errorf("skill namespace %q is not installed", namespace)
		}
		return EntryManifest{}, err
	}
	if !info.IsDir() {
		return EntryManifest{}, fmt.Errorf("skill namespace %q is not a directory", namespace)
	}
	available, err := LoadFromDir(root)
	if err != nil {
		return EntryManifest{}, err
	}
	known := make(map[string]bool, len(available)*2)
	for _, skill := range available {
		known[skill.ID] = true
		known[skill.Name] = true
	}
	normalized := make([]string, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" || seen[entry] {
			continue
		}
		if !known[entry] {
			return EntryManifest{}, fmt.Errorf("skill %q was not found in namespace %q", entry, namespace)
		}
		seen[entry] = true
		normalized = append(normalized, entry)
	}
	if len(normalized) == 0 {
		return EntryManifest{}, errors.New("at least one entry skill is required")
	}
	sort.Strings(normalized)
	path := filepath.Join(root, "superscience-skills.json")
	temporary, err := os.CreateTemp(root, ".superscience-skills-*.json")
	if err != nil {
		return EntryManifest{}, err
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{"entries": normalized}); err != nil {
		return EntryManifest{}, err
	}
	if err := temporary.Sync(); err != nil {
		return EntryManifest{}, err
	}
	if err := temporary.Close(); err != nil {
		return EntryManifest{}, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return EntryManifest{}, err
	}
	removeTemporary = false
	return EntryManifest{Namespace: namespace, Entries: normalized, Path: path}, nil
}

func applyEntryManifest(root string, records []store.SkillRecord) ([]store.SkillRecord, error) {
	data, err := os.ReadFile(filepath.Join(root, "superscience-skills.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return records, nil
		}
		return nil, err
	}
	var manifest struct {
		Entries []string `json:"entries"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("invalid skill entry manifest in %s: %w", root, err)
	}
	entries := make(map[string]bool, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		entries[strings.TrimSpace(entry)] = true
	}
	for index := range records {
		value := entries[records[index].ID] || entries[records[index].Name]
		records[index].Entry = &value
	}
	return records, nil
}

func namespaceRoot(namespace string) (string, error) {
	dataDir, err := UserDataDir()
	if err != nil {
		return "", err
	}
	if namespace == "user" {
		return filepath.Join(dataDir, "skills"), nil
	}
	return filepath.Join(dataDir, "installed-skills", namespace), nil
}

func validNamespace(namespace string) bool {
	if len(namespace) == 0 || len(namespace) > 64 {
		return false
	}
	for index, character := range namespace {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || index > 0 && (character == '-' || character == '_') {
			continue
		}
		return false
	}
	return true
}

func ParseFile(path string) (store.SkillRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return store.SkillRecord{}, err
	}
	defer file.Close()
	skill := store.SkillRecord{Path: path, Source: "filesystem", Enabled: true}
	scanner := bufio.NewScanner(file)
	inFrontMatter := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			if !inFrontMatter {
				inFrontMatter = true
				continue
			}
			break
		}
		if !inFrontMatter || !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		switch key {
		case "name":
			skill.Name = value
		case "description":
			skill.Description = value
		}
	}
	if err := scanner.Err(); err != nil {
		return store.SkillRecord{}, err
	}
	if skill.Name == "" {
		skill.Name = strings.TrimSuffix(filepath.Base(filepath.Dir(path)), filepath.Ext(path))
	}
	if skill.Description == "" {
		skill.Description = "Filesystem skill loaded from " + path
	}
	skill.ID = strings.ToLower(strings.ReplaceAll(skill.Name, " ", "-"))
	return skill, nil
}

func Load(name string, persisted []store.SkillRecord) (Loaded, error) {
	name = strings.TrimSpace(name)
	for _, skill := range Merge(Bundled(), persisted) {
		if skill.Name != name && skill.ID != name {
			continue
		}
		if !skill.Enabled {
			return Loaded{}, errors.New("skill not found")
		}
		content := bundledContent(skill)
		if skill.Path != "" {
			data, err := os.ReadFile(skill.Path)
			if err == nil && len(data) > 0 {
				content = string(data)
			}
		}
		return Loaded{SkillRecord: skill, Content: content}, nil
	}
	return Loaded{}, errors.New("skill not found")
}

func bundledContent(skill store.SkillRecord) string {
	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(skill.Name)
	builder.WriteString("\n\n")
	builder.WriteString(skill.Description)
	builder.WriteString("\n\n")
	builder.WriteString("Use this skill to ground agent behavior in explicit instructions, collect provenance, keep tool calls auditable, and return concise research artifacts.")
	return builder.String()
}
