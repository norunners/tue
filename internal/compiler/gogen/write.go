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
	if project.Root == "" {
		project.Root = root
	}
	result, diagnostics := GenerateProject(project)
	if len(diagnostics) != 0 {
		return &Manifest{GeneratedBy: "tue"}, diagnostics, nil
	}
	if result == nil {
		return nil, nil, fmt.Errorf("generate project: missing result")
	}

	cacheDir := filepath.Join(root, CacheDir)
	if err := writeCacheResult(cacheDir, *result); err != nil {
		return nil, nil, err
	}

	return &result.Manifest, nil, nil
}

func writeCacheResult(cacheDir string, result Result) error {
	if err := os.RemoveAll(cacheDir); err != nil {
		return fmt.Errorf("clean %s: %w", cacheDir, err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", cacheDir, err)
	}
	for _, file := range result.Files {
		path := filepath.Join(cacheDir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create generated dir %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, file.Source, 0o644); err != nil {
			return fmt.Errorf("write generated file %s: %w", path, err)
		}
	}
	for _, asset := range result.Assets {
		path := filepath.Join(cacheDir, filepath.FromSlash(asset.OutputPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create asset dir %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, asset.Source, 0o644); err != nil {
			return fmt.Errorf("write asset %s: %w", path, err)
		}
	}

	manifest, err := json.MarshalIndent(result.Manifest, "", "\t")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	manifest = append(manifest, '\n')
	if err := os.WriteFile(filepath.Join(cacheDir, "manifest.json"), manifest, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}
