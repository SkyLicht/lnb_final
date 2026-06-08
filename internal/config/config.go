package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	WatcherTypeFileCreated = "on_file_created"
	WatcherTypeFileUpdated = "on_file_updated"
)

type WatcherConfig struct {
	Name        string `json:"name"`
	WatcherType string `json:"watcher_type"`
	FileDir     string `json:"file_dir"`
	Function    string `json:"function"`
}

func Load(path string) ([]WatcherConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var watchers []WatcherConfig
	if err := json.Unmarshal(data, &watchers); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	if len(watchers) == 0 {
		return nil, errors.New("config must contain at least one watcher")
	}

	for i := range watchers {
		if err := normalizeAndValidate(&watchers[i]); err != nil {
			return nil, fmt.Errorf("watcher[%d]: %w", i, err)
		}
	}

	return watchers, nil
}

func normalizeAndValidate(w *WatcherConfig) error {
	w.Name = strings.TrimSpace(w.Name)
	w.WatcherType = strings.TrimSpace(w.WatcherType)
	w.FileDir = strings.TrimSpace(w.FileDir)
	w.Function = strings.TrimSpace(w.Function)

	if w.Name == "" {
		return errors.New("name is required")
	}
	if w.FileDir == "" {
		return errors.New("file_dir is required")
	}
	if w.Function == "" {
		return errors.New("function is required")
	}
	if w.WatcherType != WatcherTypeFileCreated && w.WatcherType != WatcherTypeFileUpdated {
		return fmt.Errorf("unsupported watcher_type %q", w.WatcherType)
	}

	abs, err := filepath.Abs(w.FileDir)
	if err != nil {
		return fmt.Errorf("resolve file_dir %q: %w", w.FileDir, err)
	}
	w.FileDir = abs

	return nil
}
