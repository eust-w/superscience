package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"superscience/internal/providers"
	"superscience/internal/store"
	"superscience/internal/tools"
)

var (
	ErrDefinitionConflict = errors.New("agent definition already exists")
	ErrReservedAgentID    = errors.New("agent ID is reserved by a built-in agent")
	ErrInvalidDefinition  = errors.New("invalid agent definition")
)

type Definition struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Domain       string   `json:"domain"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"systemPrompt"`
	Tools        []string `json:"tools"`
	ReviewMode   string   `json:"reviewMode"`
	Enabled      bool     `json:"enabled"`
	BuiltIn      bool     `json:"builtIn"`
	CreatedAt    int64    `json:"createdAt,omitempty"`
	UpdatedAt    int64    `json:"updatedAt,omitempty"`
}

type DefinitionPatch struct {
	Name         *string   `json:"name"`
	Domain       *string   `json:"domain"`
	Description  *string   `json:"description"`
	SystemPrompt *string   `json:"systemPrompt"`
	Tools        *[]string `json:"tools"`
	ReviewMode   *string   `json:"reviewMode"`
	Enabled      *bool     `json:"enabled"`
}

func BuiltInDefinitions() []Definition {
	builtIns := tools.BuiltInAgents()
	definitions := make([]Definition, 0, len(builtIns))
	for _, builtIn := range builtIns {
		definition := Definition{
			ID:          builtIn.ID,
			Name:        builtIn.Name,
			Domain:      builtIn.Domain,
			Description: builtIn.Description,
			Tools:       append([]string(nil), builtIn.Tools...),
			ReviewMode:  builtIn.ReviewMode,
			Enabled:     builtIn.Enabled,
			BuiltIn:     true,
		}
		definition.SystemPrompt = defaultSystemPrompt(definition)
		definitions = append(definitions, definition)
	}
	return definitions
}

func ListDefinitions(ctx context.Context, db *store.Store) ([]Definition, error) {
	definitions := BuiltInDefinitions()
	if db != nil {
		records, err := db.ListAgents(ctx)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			definitions = append(definitions, definitionFromRecord(record))
		}
	}
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].BuiltIn != definitions[j].BuiltIn {
			return definitions[i].BuiltIn
		}
		return definitions[i].Name < definitions[j].Name
	})
	return definitions, nil
}

func ResolveDefinition(ctx context.Context, db *store.Store, id string) (Definition, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "research"
	}
	for _, definition := range BuiltInDefinitions() {
		if definition.ID == id {
			if !definition.Enabled {
				return Definition{}, fmt.Errorf("agent %q is disabled", id)
			}
			return definition, nil
		}
	}
	if db == nil {
		return Definition{}, sql.ErrNoRows
	}
	record, err := db.GetAgent(ctx, id)
	if err != nil {
		return Definition{}, err
	}
	definition := definitionFromRecord(record)
	if !definition.Enabled {
		return Definition{}, fmt.Errorf("agent %q is disabled", id)
	}
	return definition, nil
}

func GetDefinition(ctx context.Context, db *store.Store, id string) (Definition, error) {
	for _, definition := range BuiltInDefinitions() {
		if definition.ID == strings.TrimSpace(id) {
			return definition, nil
		}
	}
	if db == nil {
		return Definition{}, sql.ErrNoRows
	}
	record, err := db.GetAgent(ctx, id)
	if err != nil {
		return Definition{}, err
	}
	return definitionFromRecord(record), nil
}

func CreateDefinition(ctx context.Context, db *store.Store, definition Definition) (Definition, error) {
	if db == nil {
		return Definition{}, errors.New("agent store is unavailable")
	}
	definition.BuiltIn = false
	normalized, err := normalizeDefinition(definition)
	if err != nil {
		return Definition{}, err
	}
	if isBuiltInID(normalized.ID) {
		return Definition{}, fmt.Errorf("%w: %s", ErrReservedAgentID, normalized.ID)
	}
	if _, err := db.GetAgent(ctx, normalized.ID); err == nil {
		return Definition{}, fmt.Errorf("%w: %s", ErrDefinitionConflict, normalized.ID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Definition{}, err
	}
	record, err := db.CreateAgent(ctx, recordFromDefinition(normalized))
	if err != nil {
		return Definition{}, err
	}
	return definitionFromRecord(record), nil
}

func UpdateDefinition(ctx context.Context, db *store.Store, id string, patch DefinitionPatch) (Definition, error) {
	if db == nil {
		return Definition{}, errors.New("agent store is unavailable")
	}
	id = strings.TrimSpace(id)
	if isBuiltInID(id) {
		return Definition{}, fmt.Errorf("%w: %s", ErrReservedAgentID, id)
	}
	record, err := db.GetAgent(ctx, id)
	if err != nil {
		return Definition{}, err
	}
	definition := definitionFromRecord(record)
	if patch.Name != nil {
		definition.Name = *patch.Name
	}
	if patch.Domain != nil {
		definition.Domain = *patch.Domain
	}
	if patch.Description != nil {
		definition.Description = *patch.Description
	}
	if patch.SystemPrompt != nil {
		definition.SystemPrompt = *patch.SystemPrompt
	}
	if patch.Tools != nil {
		definition.Tools = append([]string(nil), (*patch.Tools)...)
	}
	if patch.ReviewMode != nil {
		definition.ReviewMode = *patch.ReviewMode
	}
	if patch.Enabled != nil {
		definition.Enabled = *patch.Enabled
	}
	normalized, err := normalizeDefinition(definition)
	if err != nil {
		return Definition{}, err
	}
	updated, err := db.UpdateAgent(ctx, recordFromDefinition(normalized))
	if err != nil {
		return Definition{}, err
	}
	return definitionFromRecord(updated), nil
}

func DeleteDefinition(ctx context.Context, db *store.Store, id string) error {
	if db == nil {
		return errors.New("agent store is unavailable")
	}
	id = strings.TrimSpace(id)
	if isBuiltInID(id) {
		return fmt.Errorf("%w: %s", ErrReservedAgentID, id)
	}
	return db.DeleteAgent(ctx, id)
}

func SystemPromptForDefinition(definition Definition) string {
	if prompt := strings.TrimSpace(definition.SystemPrompt); prompt != "" {
		return prompt + " Use only the provided tools, preserve provenance, and never claim a tool result that was not observed."
	}
	return defaultSystemPrompt(definition)
}

func ToolDefinitionsForDefinition(definition Definition) []providers.ToolDefinition {
	return providerToolDefinitions(definition)
}

func normalizeDefinition(definition Definition) (Definition, error) {
	definition.ID = strings.TrimSpace(definition.ID)
	definition.Name = strings.TrimSpace(definition.Name)
	definition.Domain = strings.TrimSpace(definition.Domain)
	definition.Description = strings.TrimSpace(definition.Description)
	definition.SystemPrompt = strings.TrimSpace(definition.SystemPrompt)
	definition.ReviewMode = strings.ToLower(strings.TrimSpace(definition.ReviewMode))
	if definition.ReviewMode == "" {
		definition.ReviewMode = "optional"
	}
	if !validAgentID(definition.ID) {
		return Definition{}, fmt.Errorf("%w: ID must be 2-64 lowercase letters, numbers, underscores, or hyphens", ErrInvalidDefinition)
	}
	if definition.Name == "" || len(definition.Name) > 100 {
		return Definition{}, fmt.Errorf("%w: name is required and must not exceed 100 characters", ErrInvalidDefinition)
	}
	if definition.Domain == "" || len(definition.Domain) > 100 {
		return Definition{}, fmt.Errorf("%w: domain is required and must not exceed 100 characters", ErrInvalidDefinition)
	}
	if definition.Description == "" || len(definition.Description) > 2000 {
		return Definition{}, fmt.Errorf("%w: description is required and must not exceed 2000 characters", ErrInvalidDefinition)
	}
	if len(definition.SystemPrompt) > 20000 {
		return Definition{}, fmt.Errorf("%w: system prompt must not exceed 20000 characters", ErrInvalidDefinition)
	}
	switch definition.ReviewMode {
	case "none", "optional", "recommended", "required":
	default:
		return Definition{}, fmt.Errorf("%w: review mode must be none, optional, recommended, or required", ErrInvalidDefinition)
	}
	seen := make(map[string]bool, len(definition.Tools))
	normalizedTools := make([]string, 0, len(definition.Tools))
	for _, toolID := range definition.Tools {
		toolID = strings.TrimSpace(toolID)
		if toolID == "" || seen[toolID] {
			continue
		}
		if _, ok := tools.Find(toolID); !ok {
			return Definition{}, fmt.Errorf("%w: unknown tool %q", ErrInvalidDefinition, toolID)
		}
		seen[toolID] = true
		normalizedTools = append(normalizedTools, toolID)
	}
	definition.Tools = normalizedTools
	if definition.SystemPrompt == "" {
		definition.SystemPrompt = defaultSystemPrompt(definition)
	}
	return definition, nil
}

func defaultSystemPrompt(definition Definition) string {
	return "You are the Superscience " + definition.Name + " agent. " + definition.Description
}

func isBuiltInID(id string) bool {
	for _, definition := range BuiltInDefinitions() {
		if definition.ID == id {
			return true
		}
	}
	return false
}

func validAgentID(id string) bool {
	if len(id) < 2 || len(id) > 64 {
		return false
	}
	for index, char := range id {
		if unicode.IsLower(char) || unicode.IsDigit(char) || (index > 0 && (char == '-' || char == '_')) {
			continue
		}
		return false
	}
	return true
}

func definitionFromRecord(record store.AgentRecord) Definition {
	return Definition{
		ID: record.ID, Name: record.Name, Domain: record.Domain, Description: record.Description, SystemPrompt: record.SystemPrompt,
		Tools: append([]string(nil), record.Tools...), ReviewMode: record.ReviewMode, Enabled: record.Enabled, BuiltIn: record.BuiltIn,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func recordFromDefinition(definition Definition) store.AgentRecord {
	return store.AgentRecord{
		ID: definition.ID, Name: definition.Name, Domain: definition.Domain, Description: definition.Description, SystemPrompt: definition.SystemPrompt,
		Tools: append([]string(nil), definition.Tools...), ReviewMode: definition.ReviewMode, Enabled: definition.Enabled, BuiltIn: definition.BuiltIn,
		CreatedAt: definition.CreatedAt, UpdatedAt: definition.UpdatedAt,
	}
}
