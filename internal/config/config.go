package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Version   int      `yaml:"version"`
	Baseline  []string `yaml:"baseline"`
	Design    []string `yaml:"design"`
	Context   []string `yaml:"context"`
	OutputDir string   `yaml:"output_dir"`
	LLM       struct {
		BridgeCommand []string `yaml:"bridge_command"`
		Model         string   `yaml:"model"`
		Effort        string   `yaml:"effort"`
		Concurrency   int      `yaml:"concurrency"`
	} `yaml:"llm"`
	Jev struct {
		Model       string `yaml:"model"`
		Concurrency int    `yaml:"concurrency"`
	} `yaml:"jev"`
	Path string `yaml:"-"`
	Root string `yaml:"-"`
}

const CodexModel = "gpt-6-sol"
const CodexEffort = "medium"

func Load(path string) (Config, error) {
	var cfg Config
	abs, err := filepath.Abs(path)
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return cfg, err
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, err
	}
	if cfg.Version != 1 {
		return cfg, fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if len(cfg.Baseline) == 0 || len(cfg.Design) == 0 {
		return cfg, errors.New("baseline and design must be nonempty")
	}
	if len(cfg.LLM.BridgeCommand) == 0 {
		cfg.LLM.BridgeCommand = []string{"node", "./bridge/dist/src/main.js"}
	}
	if cfg.LLM.Model != "" && cfg.LLM.Model != CodexModel {
		return cfg, fmt.Errorf("llm.model must be %s", CodexModel)
	}
	if cfg.LLM.Effort != "" && cfg.LLM.Effort != CodexEffort {
		return cfg, fmt.Errorf("llm.effort must be %s", CodexEffort)
	}
	cfg.LLM.Model = CodexModel
	cfg.LLM.Effort = CodexEffort
	if cfg.Jev.Model == "" {
		cfg.Jev.Model = "jev-latest"
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = ".specaudit"
	}
	if cfg.LLM.Concurrency <= 0 {
		cfg.LLM.Concurrency = 1
	}
	if cfg.LLM.Concurrency != 1 {
		return cfg, errors.New("llm.concurrency must be 1 in this version")
	}
	if cfg.Jev.Concurrency <= 0 {
		cfg.Jev.Concurrency = 4
	}
	cfg.Path, cfg.Root = abs, filepath.Dir(abs)
	return cfg, nil
}

func (c Config) Resolve(patterns []string) ([]string, error) {
	seen := make(map[string]bool)
	var paths []string
	for _, pattern := range patterns {
		if pattern == "" {
			return nil, errors.New("empty path pattern")
		}
		absolute := pattern
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(c.Root, absolute)
		}
		matches, err := doublestar.FilepathGlob(absolute)
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("pattern %q matched no files", pattern)
		}
		for _, path := range matches {
			info, err := os.Stat(path)
			if err != nil {
				return nil, err
			}
			if info.IsDir() || filepath.Ext(path) != ".md" {
				continue
			}
			path, err = filepath.Abs(path)
			if err != nil {
				return nil, err
			}
			if !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
		}
	}
	if len(paths) == 0 {
		return nil, errors.New("no Markdown files matched")
	}
	slicesSort(paths)
	return paths, nil
}

func slicesSort(paths []string) {
	for i := 1; i < len(paths); i++ {
		for j := i; j > 0 && paths[j] < paths[j-1]; j-- {
			paths[j], paths[j-1] = paths[j-1], paths[j]
		}
	}
}
