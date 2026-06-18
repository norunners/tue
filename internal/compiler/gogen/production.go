package gogen

import (
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
)

const (
	DistDir        = "dist"
	wasmFilePath   = "app.wasm"
	loaderFilePath = "tue_loader.js"
	indexFilePath  = "index.html"
	manifestPath   = "manifest.json"

	generatedModulePath = "tue.local/generated"
	tueModulePath       = "github.com/norunners/tue"
	tueModuleReplaceEnv = "TUE_MODULE_REPLACE"
)

// ProductionBuild is the generated static build output for a project.
type ProductionBuild struct {
	Manifest      Manifest
	Files         []string
	WASMSizeBytes int64
}

// WriteProductionProject writes generated cache files and a static dist build.
func WriteProductionProject(root string, project Project) (*ProductionBuild, []Diagnostic, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve project root %s: %w", root, err)
	}
	root = absRoot
	if project.Root == "" {
		project.Root = root
	} else if !filepath.IsAbs(project.Root) {
		absProjectRoot, err := filepath.Abs(project.Root)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve project root %s: %w", project.Root, err)
		}
		project.Root = absProjectRoot
	}
	result, diagnostics := GenerateProject(project)
	if len(diagnostics) != 0 {
		return &ProductionBuild{Manifest: Manifest{GeneratedBy: "tue"}}, diagnostics, nil
	}
	if result == nil {
		return nil, nil, fmt.Errorf("generate project: missing result")
	}
	if _, _, err := productionManifest(result.Manifest); err != nil {
		return nil, nil, err
	}

	cacheDir := filepath.Join(root, CacheDir)
	if err := writeCacheResult(cacheDir, *result); err != nil {
		return nil, nil, err
	}
	if err := writeProductionModule(cacheDir, result.Manifest); err != nil {
		return nil, nil, err
	}

	distDir := filepath.Join(root, DistDir)
	if err := os.RemoveAll(distDir); err != nil {
		return nil, nil, fmt.Errorf("clean %s: %w", distDir, err)
	}
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create %s: %w", distDir, err)
	}

	wasmPath := filepath.Join(distDir, wasmFilePath)
	if err := buildProductionWASM(cacheDir, wasmPath); err != nil {
		return nil, nil, err
	}
	wasmInfo, err := os.Stat(wasmPath)
	if err != nil {
		return nil, nil, fmt.Errorf("stat %s: %w", wasmPath, err)
	}

	manifest, files, err := writeDistFiles(distDir, *result)
	if err != nil {
		return nil, nil, err
	}
	return &ProductionBuild{
		Manifest:      manifest,
		Files:         files,
		WASMSizeBytes: wasmInfo.Size(),
	}, nil, nil
}

func writeProductionModule(cacheDir string, manifest Manifest) error {
	component, err := productionEntryComponent(manifest)
	if err != nil {
		return err
	}

	source, err := productionMainSource(component)
	if err != nil {
		return err
	}
	cmdDir := filepath.Join(cacheDir, "cmd", "tue_app")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		return fmt.Errorf("create production entry dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), source, 0o644); err != nil {
		return fmt.Errorf("write production entrypoint: %w", err)
	}

	dependency, err := tueModuleDependency()
	if err != nil {
		return err
	}
	goMod := productionGoMod(dependency)
	if err := os.WriteFile(filepath.Join(cacheDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return fmt.Errorf("write production go.mod: %w", err)
	}
	return nil
}

func productionEntryComponent(manifest Manifest) (string, error) {
	if len(manifest.Files) == 0 {
		return "", fmt.Errorf("production build requires at least one component")
	}
	for _, file := range manifest.Files {
		if file.Component == "App" {
			return file.Component, nil
		}
	}
	return manifest.Files[0].Component, nil
}

func productionMainSource(component string) ([]byte, error) {
	source := []byte(fmt.Sprintf(`package main

import (
	"log"

	app %q
	"github.com/norunners/tue"
)

func main() {
	if _, err := tue.Mount("#app", app.New%s()); err != nil {
		log.Fatal(err)
	}
	select {}
}
`, generatedModulePath, component))
	formatted, err := format.Source(source)
	if err != nil {
		return nil, fmt.Errorf("format production entrypoint: %w", err)
	}
	return formatted, nil
}

type productionModuleDependency struct {
	Version string
	Replace string
}

func productionGoMod(dependency productionModuleDependency) string {
	goMod := fmt.Sprintf("module %s\n\ngo %s\n\nrequire %s %s\n", generatedModulePath, goDirective(), tueModulePath, dependency.Version)
	if dependency.Replace != "" {
		goMod += fmt.Sprintf("\nreplace %s => %s\n", tueModulePath, filepath.ToSlash(dependency.Replace))
	}
	return goMod
}

func tueModuleDependency() (productionModuleDependency, error) {
	buildInfo, _ := debug.ReadBuildInfo()
	return resolveTueModuleDependency(buildInfo, os.Getenv(tueModuleReplaceEnv), tueModuleRoot)
}

func resolveTueModuleDependency(buildInfo *debug.BuildInfo, replaceOverride string, moduleRoot func() (string, error)) (productionModuleDependency, error) {
	if replaceOverride != "" {
		return productionModuleDependency{Version: "v0.0.0", Replace: replaceOverride}, nil
	}
	if version := tueBuildInfoVersion(buildInfo); version != "" {
		return productionModuleDependency{Version: version}, nil
	}

	root, err := moduleRoot()
	if err != nil {
		return productionModuleDependency{}, fmt.Errorf("resolve Tue module dependency: %w", err)
	}
	return productionModuleDependency{Version: "v0.0.0", Replace: root}, nil
}

func tueBuildInfoVersion(buildInfo *debug.BuildInfo) string {
	if buildInfo == nil {
		return ""
	}
	if buildInfo.Main.Path == tueModulePath && usableTueModuleVersion(buildInfo.Main.Version) {
		return buildInfo.Main.Version
	}
	for _, dependency := range buildInfo.Deps {
		if dependency.Path == tueModulePath && usableTueModuleVersion(dependency.Version) {
			return dependency.Version
		}
	}
	return ""
}

func usableTueModuleVersion(version string) bool {
	return version != "" && version != "(devel)" && !strings.Contains(version, "+dirty")
}

func tueModuleRoot() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve Tue module root: runtime caller unavailable")
	}
	dir := filepath.Dir(filename)
	for {
		goModPath := filepath.Join(dir, "go.mod")
		source, err := os.ReadFile(goModPath)
		if err == nil && strings.Contains(string(source), "module "+tueModulePath) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("resolve Tue module root from %s", filename)
}

func goDirective() string {
	version := strings.TrimPrefix(runtime.Version(), "go")
	if version == "" || strings.Contains(version, "devel") {
		return "1.26"
	}
	return version
}

func buildProductionWASM(cacheDir string, outputPath string) error {
	command := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", outputPath, "./cmd/tue_app")
	command.Dir = cacheDir
	command.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	combined, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build %s: %w\n%s", wasmFilePath, err, combined)
	}
	return nil
}

func writeDistFiles(distDir string, result Result) (Manifest, []string, error) {
	files := []string{wasmFilePath}
	manifest, assetOutputs, err := productionManifest(result.Manifest)
	if err != nil {
		return Manifest{}, nil, err
	}

	staticFiles := map[string][]byte{
		indexFilePath:  productionIndexSource(),
		styleFilePath:  productionStyleSource(result),
		loaderFilePath: nil,
		manifestPath:   nil,
	}

	loader, err := productionLoaderSource()
	if err != nil {
		return Manifest{}, nil, err
	}
	staticFiles[loaderFilePath] = loader

	manifestSource, err := json.MarshalIndent(manifest, "", "\t")
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("encode production manifest: %w", err)
	}
	staticFiles[manifestPath] = append(manifestSource, '\n')

	for path, source := range staticFiles {
		if err := writeDistFile(distDir, path, source); err != nil {
			return Manifest{}, nil, err
		}
		files = append(files, path)
	}

	for _, asset := range result.Assets {
		output := assetOutputs[asset.SourcePath]
		if err := writeDistFile(distDir, output, asset.Source); err != nil {
			return Manifest{}, nil, err
		}
		files = append(files, output)
	}

	sort.Strings(files)
	return manifest, files, nil
}

func productionManifest(manifest Manifest) (Manifest, map[string]string, error) {
	next := manifest
	next.StyleFile = styleFilePath
	if len(manifest.Assets) == 0 {
		return next, nil, nil
	}

	assetOutputs := make(map[string]string, len(manifest.Assets))
	next.Assets = make([]ManifestAsset, len(manifest.Assets))
	reserved := productionReservedFiles()
	used := make(map[string]string, len(manifest.Assets))
	for i, asset := range manifest.Assets {
		output := productionAssetOutputPath(asset)
		if previous, ok := reserved[output]; ok {
			if asset.Public {
				return Manifest{}, nil, fmt.Errorf("public asset %q conflicts with generated production file %q", asset.Source, previous)
			}
			return Manifest{}, nil, fmt.Errorf("asset %q conflicts with generated production file %q", asset.Source, previous)
		}
		if previous, ok := used[output]; ok {
			return Manifest{}, nil, fmt.Errorf("asset %q conflicts with %q at production output %q", asset.Source, previous, output)
		}
		used[output] = asset.Source
		assetOutputs[asset.Source] = output
		next.Assets[i] = ManifestAsset{
			Source: asset.Source,
			Output: output,
			Public: asset.Public,
		}
	}
	return next, assetOutputs, nil
}

func productionReservedFiles() map[string]string {
	return map[string]string{
		indexFilePath:  indexFilePath,
		wasmFilePath:   wasmFilePath,
		loaderFilePath: loaderFilePath,
		styleFilePath:  styleFilePath,
		manifestPath:   manifestPath,
	}
}

func productionAssetOutputPath(asset ManifestAsset) string {
	if asset.Public {
		return strings.TrimPrefix(asset.Output, publicAssetOutputDir+"/")
	}
	return asset.Output
}

func productionIndexSource() []byte {
	return []byte(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Tue App</title>
	<link rel="stylesheet" href="style.css">
	<script src="tue_loader.js" defer></script>
</head>
<body>
	<div id="app"></div>
</body>
</html>
`)
}

func productionStyleSource(result Result) []byte {
	for _, file := range result.Files {
		if file.Path == styleFilePath {
			return file.Source
		}
	}
	return nil
}

func productionLoaderSource() ([]byte, error) {
	wasmExec, err := os.ReadFile(wasmExecPath())
	if err != nil {
		return nil, fmt.Errorf("read wasm_exec.js: %w", err)
	}
	loader := []byte(`

(function () {
	"use strict";

	const go = new Go();

	async function instantiate() {
		const response = await fetch("app.wasm");
		if (WebAssembly.instantiateStreaming) {
			try {
				return await WebAssembly.instantiateStreaming(response.clone(), go.importObject);
			} catch (error) {
				console.warn("tue: falling back to ArrayBuffer WASM loading", error);
			}
		}
		const bytes = await response.arrayBuffer();
		return await WebAssembly.instantiate(bytes, go.importObject);
	}

	instantiate()
		.then((result) => go.run(result.instance))
		.catch((error) => {
			console.error("tue: failed to start app.wasm", error);
		});
})();
`)
	return append(wasmExec, loader...), nil
}

func wasmExecPath() string {
	goRoot := runtime.GOROOT()
	for _, relative := range []string{
		filepath.Join("lib", "wasm", "wasm_exec.js"),
		filepath.Join("misc", "wasm", "wasm_exec.js"),
	} {
		path := filepath.Join(goRoot, relative)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return filepath.Join(goRoot, "lib", "wasm", "wasm_exec.js")
}

func writeDistFile(distDir string, path string, source []byte) error {
	output := filepath.Join(distDir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create dist dir %s: %w", filepath.Dir(output), err)
	}
	if err := os.WriteFile(output, source, 0o644); err != nil {
		return fmt.Errorf("write dist file %s: %w", output, err)
	}
	return nil
}
