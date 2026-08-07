package skills_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"superscience/internal/skills"
	"superscience/internal/store"
)

func TestSkillEntryManifestControlsPickerVisibilityWithoutDisablingLoad(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("SUPERSCIENCE_DATA_DIR", dataDir)
	for _, name := range []string{"public-evidence", "internal-helper"} {
		dir := filepath.Join(dataDir, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: " + name + " instructions.\n---\n# " + name + "\n\nPreserve provenance.\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := skills.SetEntries("user", []string{"public-evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Namespace != "user" || len(manifest.Entries) != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
	records, err := skills.LoadUser()
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]bool{}
	for _, record := range records {
		entries[record.Name] = skills.IsEntry(record)
	}
	if !entries["public-evidence"] || entries["internal-helper"] {
		t.Fatalf("entry visibility = %+v", entries)
	}
	loaded, err := skills.Load("internal-helper", records)
	if err != nil || loaded.Name != "internal-helper" {
		t.Fatalf("hidden helper should remain loadable: %+v err=%v", loaded, err)
	}
	if _, err := skills.SetEntries("user", []string{"missing-skill"}); err == nil {
		t.Fatal("unknown entry skill was accepted")
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "skills", "superscience-skills.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string][]string
	if err := json.Unmarshal(data, &persisted); err != nil || len(persisted["entries"]) != 1 {
		t.Fatalf("persisted manifest = %s err=%v", data, err)
	}
}

func TestLoadSkipsDisabledPersistedSkills(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: disabled-skill\ndescription: Disabled skill.\n---\n# Disabled\nDo not load.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := skills.Load("disabled-skill", []store.SkillRecord{{
		ID:          "disabled-skill",
		Name:        "disabled-skill",
		Description: "Disabled skill.",
		Source:      "project",
		Path:        path,
		Enabled:     false,
	}})
	if err == nil {
		t.Fatal("disabled persisted skill loaded successfully")
	}
}

func TestLoadDisabledPersistedSkillShadowsBundledSkill(t *testing.T) {
	_, err := skills.Load("research-protocol", []store.SkillRecord{{
		ID:          "research-protocol",
		Name:        "research-protocol",
		Description: "Disabled bundled override.",
		Source:      "user",
		Enabled:     false,
	}})
	if err == nil {
		t.Fatal("disabled persisted bundled override loaded successfully")
	}
}

func TestLoadUserDiscoversSkillsFromDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("SUPERSCIENCE_DATA_DIR", dataDir)
	skillDir := filepath.Join(dataDir, "skills", "user-evidence")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: user-evidence\ndescription: User evidence skill.\n---\n# User Evidence\nUse globally.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := skills.LoadUser()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Name != "user-evidence" || records[0].Source != "user" || records[0].Path != skillPath {
		t.Fatalf("user skills = %+v", records)
	}
}
