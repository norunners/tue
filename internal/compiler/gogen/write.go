package gogen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const CacheDir = ".tue-cache"

// WriteProject writes generated project output under root/.tue-cache.
func WriteProject(root string, project Project) (*Manifest, []Diagnostic, error) {
	result, diagnostics := GenerateProject(project)
	if len(diagnostics) != 0 {
		return nil, diagnostics, nil
	}

	cacheDir := filepath.Join(root, CacheDir)
	if err := os.RemoveAll(cacheDir); err != nil {
		return nil, nil, fmt.Errorf("clean %s: %w", cacheDir, err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create %s: %w", cacheDir, err)
	}

	for _, file := range result.Files {
		path := filepath.Join(cacheDir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, nil, fmt.Errorf("create generated dir %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, file.Source, 0o644); err != nil {
			return nil, nil, fmt.Errorf("write generated file %s: %w", path, err)
		}
	}

	manifest, err := json.MarshalIndent(result.Manifest, "", "\t")
	if err != nil {
		return nil, nil, fmt.Errorf("encode manifest: %w", err)
	}
	manifest = append(manifest, '\n')
	if err := os.WriteFile(filepath.Join(cacheDir, "manifest.json"), manifest, 0o644); err != nil {
		return nil, nil, fmt.Errorf("write manifest: %w", err)
	}

	return &result.Manifest, nil, nil
}
