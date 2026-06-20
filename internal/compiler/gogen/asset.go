package gogen

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/norunners/tue/internal/compiler/sfc"
	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

const (
	assetOutputDir       = "assets"
	publicAssetOutputDir = "public"
)

type assetPipeline struct {
	root        string
	bySource    map[string]*GeneratedAsset
	byOutput    map[string]*GeneratedAsset
	diagnostics []Diagnostic
}

func newAssetPipeline(root string) *assetPipeline {
	if root == "" {
		return nil
	}
	return &assetPipeline{
		root:     root,
		bySource: make(map[string]*GeneratedAsset),
		byOutput: make(map[string]*GeneratedAsset),
	}
}

func (p *assetPipeline) rewriteURL(filePath string, rawURL string, span sfc.Span) (string, bool) {
	if p == nil || !isLocalAssetURL(rawURL) {
		return rawURL, true
	}

	ref, ok := splitAssetURL(rawURL)
	if !ok {
		return rawURL, true
	}

	sourcePath, outputURL, public, ok := assetPathsForURL(filePath, ref.Path, ref.Suffix)
	if !ok {
		p.add(filePath, fmt.Sprintf("asset %q resolves outside the project root", rawURL), span)
		return rawURL, false
	}

	asset, err := p.addAsset(sourcePath, public)
	if err != nil {
		p.add(filePath, err.Error(), span)
		return rawURL, false
	}
	if !public {
		outputURL = asset.OutputPath + ref.Suffix
	}
	return outputURL, true
}

func (p *assetPipeline) rewriteStyleURLs(file File) bool {
	if p == nil || file.Style == nil || strings.TrimSpace(file.Style.Source) == "" {
		return true
	}

	rewritten, ok := p.rewriteCSSURLs(filePath(file), file.Style.Source, file.Style.Span)
	if !ok {
		return false
	}
	file.Style.Source = rewritten
	return true
}

func (p *assetPipeline) rewriteCSSURLs(filePath string, source string, span sfc.Span) (string, bool) {
	rewritten, ok := rewriteCSSURLs(source, func(rawURL string) (string, bool) {
		return p.rewriteURL(filePath, rawURL, span)
	})
	if !ok {
		p.add(filePath, "rewrite CSS asset URLs: malformed CSS", span)
		return source, false
	}
	return rewritten, true
}

func (p *assetPipeline) collectPublicAssets() {
	if p == nil {
		return
	}

	publicRoot := filepath.Join(p.root, "public")
	info, err := os.Stat(publicRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		p.add("", fmt.Sprintf("stat public directory: %v", err), sfc.Span{})
		return
	}
	if !info.IsDir() {
		p.add("", "public path exists but is not a directory", sfc.Span{})
		return
	}

	err = filepath.WalkDir(publicRoot, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		relativePath, err := filepath.Rel(publicRoot, filename)
		if err != nil {
			return err
		}
		sourcePath := path.Join("public", filepath.ToSlash(relativePath))
		if _, err := p.addAsset(sourcePath, true); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		p.add("", fmt.Sprintf("copy public assets: %v", err), sfc.Span{})
	}
}

func (p *assetPipeline) sortedAssets() []GeneratedAsset {
	if p == nil || len(p.byOutput) == 0 {
		return nil
	}

	assets := make([]GeneratedAsset, 0, len(p.byOutput))
	for _, asset := range p.byOutput {
		assets = append(assets, *asset)
	}
	sort.SliceStable(assets, func(i int, j int) bool {
		if assets[i].OutputPath != assets[j].OutputPath {
			return assets[i].OutputPath < assets[j].OutputPath
		}
		return assets[i].SourcePath < assets[j].SourcePath
	})
	return assets
}

func (p *assetPipeline) addAsset(sourcePath string, public bool) (*GeneratedAsset, error) {
	sourcePath = cleanAssetPath(sourcePath)
	if asset, ok := p.bySource[sourcePath]; ok {
		return asset, nil
	}

	source, err := os.ReadFile(filepath.Join(p.root, filepath.FromSlash(sourcePath)))
	if err != nil {
		return nil, fmt.Errorf("read asset %q: %w", sourcePath, err)
	}

	outputPath := publicAssetOutputPath(sourcePath)
	if !public {
		outputPath = hashedAssetOutputPath(sourcePath, source)
	}

	asset := &GeneratedAsset{
		SourcePath: sourcePath,
		OutputPath: outputPath,
		Source:     source,
		Public:     public,
	}
	if previous, ok := p.byOutput[outputPath]; ok {
		if previous.SourcePath == sourcePath {
			p.bySource[sourcePath] = previous
			return previous, nil
		}
		return nil, fmt.Errorf("asset %q conflicts with %q at output %q", sourcePath, previous.SourcePath, outputPath)
	}
	p.bySource[sourcePath] = asset
	p.byOutput[outputPath] = asset
	return asset, nil
}

func (p *assetPipeline) add(path string, message string, span sfc.Span) {
	p.diagnostics = append(p.diagnostics, Diagnostic{
		Path:    path,
		Message: message,
		Span:    span,
	})
}

type assetURL struct {
	Path   string
	Suffix string
}

func splitAssetURL(rawURL string) (*assetURL, bool) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, false
	}

	cut := len(trimmed)
	for _, marker := range []string{"?", "#"} {
		if index := strings.Index(trimmed, marker); index != -1 && index < cut {
			cut = index
		}
	}
	if cut == 0 {
		return nil, false
	}
	return &assetURL{
		Path:   trimmed[:cut],
		Suffix: trimmed[cut:],
	}, true
}

func assetPathsForURL(filePath string, rawPath string, suffix string) (string, string, bool, bool) {
	if strings.HasPrefix(rawPath, "/") {
		publicPath := cleanAssetPath(strings.TrimPrefix(rawPath, "/"))
		if publicPath == "." || strings.HasPrefix(publicPath, "../") {
			return "", "", false, false
		}
		return path.Join("public", publicPath), "/" + publicPath + suffix, true, true
	}

	sourceDir := path.Dir(filepath.ToSlash(filePath))
	if sourceDir == "." {
		sourceDir = ""
	}
	sourcePath := cleanAssetPath(path.Join(sourceDir, rawPath))
	if sourcePath == "." || sourcePath == ".." || strings.HasPrefix(sourcePath, "../") {
		return "", "", false, false
	}
	return sourcePath, "", false, true
}

func cleanAssetPath(value string) string {
	return path.Clean(filepath.ToSlash(value))
}

func isLocalAssetURL(rawURL string) bool {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return false
	}
	if strings.HasPrefix(trimmed, "/") {
		return true
	}

	colon := strings.Index(trimmed, ":")
	slash := strings.Index(trimmed, "/")
	if colon != -1 && (slash == -1 || colon < slash) {
		return false
	}
	return true
}

func hashedAssetOutputPath(sourcePath string, source []byte) string {
	extension := path.Ext(sourcePath)
	name := strings.TrimSuffix(path.Base(sourcePath), extension)
	hash := sha256.Sum256(source)
	return path.Join(assetOutputDir, fmt.Sprintf("%s.%x%s", name, hash[:4], extension))
}

func publicAssetOutputPath(sourcePath string) string {
	return path.Join(publicAssetOutputDir, strings.TrimPrefix(cleanAssetPath(sourcePath), "public/"))
}

type cssURLRewriter func(rawURL string) (string, bool)

func rewriteCSSURLs(source string, rewrite cssURLRewriter) (string, bool) {
	lexer := css.NewLexer(parse.NewInputString(source))
	offset := 0
	replacements := make([]cssReplacement, 0)

	for {
		token, data := lexer.Next()
		if token == css.ErrorToken {
			if lexer.Err() == io.EOF {
				return applyCSSReplacements(source, replacements), true
			}
			return source, false
		}
		rawToken := string(data)
		tokenStart := strings.Index(source[offset:], rawToken)
		if tokenStart == -1 {
			return source, false
		}
		start := offset + tokenStart
		end := start + len(rawToken)
		offset = end

		if token != css.URLToken {
			continue
		}
		rewritten, ok := rewriteCSSURLToken(rawToken, rewrite)
		if !ok {
			return source, false
		}
		if rewritten != rawToken {
			replacements = append(replacements, cssReplacement{
				start: start,
				end:   end,
				value: rewritten,
			})
		}
	}
}

func rewriteCSSURLToken(rawToken string, rewrite cssURLRewriter) (string, bool) {
	open := strings.Index(rawToken, "(")
	close := strings.LastIndex(rawToken, ")")
	if open == -1 || close == -1 || close <= open {
		return rawToken, true
	}

	rawValue := rawToken[open+1 : close]
	prefixLength := leadingWhitespaceLength(rawValue)
	suffixLength := trailingWhitespaceLength(rawValue)
	value := rawValue[prefixLength : len(rawValue)-suffixLength]
	quote := byte(0)
	if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] {
		quote = value[0]
		value = value[1 : len(value)-1]
	}

	rewritten, ok := rewrite(value)
	if !ok {
		return rawToken, false
	}
	if rewritten == value {
		return rawToken, true
	}
	if quote == 0 {
		quote = '"'
	}
	return rawToken[:open+1] + rawValue[:prefixLength] + string(quote) + rewritten + string(quote) + rawValue[len(rawValue)-suffixLength:] + rawToken[close:], true
}

func leadingWhitespaceLength(value string) int {
	for i, r := range value {
		if !unicode.IsSpace(r) {
			return i
		}
	}
	return len(value)
}

func trailingWhitespaceLength(value string) int {
	for i := len(value); i > 0; {
		r, size := utf8.DecodeLastRuneInString(value[:i])
		if !unicode.IsSpace(r) {
			return len(value) - i
		}
		i -= size
	}
	return len(value)
}
