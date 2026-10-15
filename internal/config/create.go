package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// CreateConfig contains inputs, never runtime identities or credentials.
type CreateConfig struct {
	CreateSelection
	Repository Repository `json:"repository,omitempty"`
	Workspace  string     `json:"workspace"`
	Memory     string     `json:"memory,omitempty"`
	Ports      []string   `json:"ports,omitempty"`
	Namespace  string     `json:"namespace,omitempty"`
	PaseoHome  string     `json:"paseo_home,omitempty"`
	Build      *bool      `json:"build,omitempty"`
	Apply      bool       `json:"apply,omitempty"`
}

// A distinct type avoids Selection's legacy JSON decoder swallowing creation fields.
type CreateSelection Selection

func (c CreateConfig) Selection() Selection { return Selection(c.CreateSelection) }

type Repository struct {
	URL string `json:"url"`
	Ref string `json:"ref,omitempty"`
}

func LoadCreateConfig(path string) (CreateConfig, error) {
	var c CreateConfig
	path, err := Canonical(path)
	if err != nil {
		return c, err
	}
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, fmt.Errorf("create config: %w", err)
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("create config must contain one JSON object")
	}
	if c.Schema == 0 {
		c.Schema = 2
	}
	if c.Schema != 2 {
		return c, fmt.Errorf("create config requires schema 2")
	}
	if c.Build == nil {
		build := true
		c.Build = &build
	}
	for _, value := range []*string{&c.Workspace, &c.PaseoHome} {
		if *value == "" {
			continue
		}
		p := *value
		if !filepath.IsAbs(p) && p != "~" && !strings.HasPrefix(p, "~/") {
			p = filepath.Join(filepath.Dir(path), p)
		}
		*value, err = Canonical(p)
		if err != nil {
			return c, err
		}
	}
	if c.Repository.Ref != "" && c.Repository.URL == "" {
		return c, fmt.Errorf("repository.ref requires repository.url")
	}
	return c, nil
}

var memoryPattern = regexp.MustCompile(`^[1-9][0-9]*[kKmMgG]?[bB]?$`)

func ValidateMemory(category Category, memory string) error {
	if memory == "" {
		return nil
	}
	if category != CategorySandbox {
		return fmt.Errorf("memory is supported only for sandbox; container shares host resources without a per-container limit")
	}
	if !memoryPattern.MatchString(memory) {
		return fmt.Errorf("sandbox memory: expected a positive size such as 2g or 4096m")
	}
	return nil
}
