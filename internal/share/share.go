package share

import (
	"encoding/json"
	"time"

	"superscience/internal/store"
)

type PublishRequest struct {
	Title       string   `json:"title"`
	ArtifactIDs []string `json:"artifactIds"`
	Visibility  string   `json:"visibility"`
	SessionID   string   `json:"sessionId,omitempty"`
}

type Bundle struct {
	Schema     string           `json:"schema"`
	Title      string           `json:"title"`
	Visibility string           `json:"visibility"`
	Artifacts  []store.Artifact `json:"artifacts"`
	CreatedAt  int64            `json:"createdAt"`
}

func BuildBundle(request PublishRequest, artifacts []store.Artifact) (string, error) {
	visibility := request.Visibility
	if visibility == "" {
		visibility = "private-link"
	}
	title := request.Title
	if title == "" {
		title = "Superscience share"
	}
	bundle := Bundle{
		Schema:     "superscience.share.v1",
		Title:      title,
		Visibility: visibility,
		Artifacts:  artifacts,
		CreatedAt:  time.Now().UnixMilli(),
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
