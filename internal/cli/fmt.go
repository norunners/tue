package cli

import (
	"bytes"
	"fmt"
	goformat "go/format"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/norunners/tue/internal/compiler/checker"
	"github.com/norunners/tue/internal/compiler/sfc"
	compilerTemplate "github.com/norunners/tue/internal/compiler/template"
)

func runFmt(args []string, stdout, stderr io.Writer) int {
	root, code, ok := parseProjectRoot("fmt", args, stdout, stderr)
	if !ok {
		return code
	}

	files, err := discoverTueFiles(root)
	if err != nil {
		fmt.Fprintf(stderr, "tue fmt: %v\n", err)
		return exitError
	}

	formattedFiles, diagnostics, err := formatTueFiles(root, files)
	if err != nil {
		fmt.Fprintf(stderr, "tue fmt: %v\n", err)
		return exitError
	}
	if len(diagnostics) != 0 {
		printDiagnostics(stderr, diagnostics)
		return exitError
	}

	for _, file := range formattedFiles {
		if !file.changed {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(file.path)), file.source, 0o644); err != nil {
			fmt.Fprintf(stderr, "tue fmt: write %s: %v\n", file.path, err)
			return exitError
		}
	}

	fmt.Fprintf(stdout, "tue fmt: formatted %d .tue file(s) in %s\n", len(files), filepath.Clean(root))
	return exitOK
}

type formattedTueFile struct {
	path    string
	source  []byte
	changed bool
}

func formatTueFiles(root string, paths []string) ([]formattedTueFile, []checker.Diagnostic, error) {
	files := make([]formattedTueFile, 0, len(paths))
	var diagnostics []checker.Diagnostic

	for _, path := range paths {
		source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", path, err)
		}

		formatted, fileDiagnostics, err := formatTueSource(path, source)
		if err != nil {
			return nil, nil, err
		}
		diagnostics = append(diagnostics, fileDiagnostics...)
		if len(fileDiagnostics) != 0 {
			continue
		}
		files = append(files, formattedTueFile{
			path:    path,
			source:  formatted,
			changed: !bytes.Equal(source, formatted),
		})
	}

	return files, diagnostics, nil
}

func formatTueSource(path string, source []byte) ([]byte, []checker.Diagnostic, error) {
	file, sfcDiagnostics := sfc.Parse(path, source)
	if len(sfcDiagnostics) != 0 {
		return nil, sfcDiagnosticsFor(path, sfcDiagnostics), nil
	}

	blockContents := make(map[*sfc.Block]string, len(file.Blocks))
	for _, block := range file.Blocks {
		content, diagnostics, err := formatTueBlock(path, block)
		if err != nil || len(diagnostics) != 0 {
			return nil, diagnostics, err
		}
		blockContents[block] = content
	}

	return rebuildFormattedTueSource(source, file, blockContents), nil, nil
}

func formatTueBlock(path string, block *sfc.Block) (string, []checker.Diagnostic, error) {
	switch block.Kind {
	case sfc.BlockTemplate:
		if _, diagnostics := compilerTemplate.ParseBlock(block); len(diagnostics) != 0 {
			return "", templateDiagnosticsFor(path, diagnostics), nil
		}
		return formatTemplateContent(block.Content), nil, nil
	case sfc.BlockScript:
		formatted, err := goformat.Source([]byte(trimBlockBoundaryNewlines(block.Content)))
		if err != nil {
			return "", []checker.Diagnostic{{
				Path:    path,
				Message: fmt.Sprintf("format script block: %v", err),
				Span:    block.ContentSpan,
			}}, nil
		}
		return strings.TrimRight(string(formatted), "\n"), nil, nil
	case sfc.BlockStyle:
		return trimBlockBoundaryNewlines(block.Content), nil, nil
	default:
		return trimBlockBoundaryNewlines(block.Content), nil, nil
	}
}

func rebuildFormattedTueSource(source []byte, file *sfc.File, blockContents map[*sfc.Block]string) []byte {
	var builder strings.Builder
	for i, block := range file.Blocks {
		if i > 0 {
			builder.WriteString("\n\n")
		}

		builder.WriteString(sourceSpan(source, block.OpenTagSpan))
		builder.WriteByte('\n')
		if content := blockContents[block]; content != "" {
			builder.WriteString(content)
			builder.WriteByte('\n')
		}
		builder.WriteString(sourceSpan(source, block.CloseTagSpan))
	}
	builder.WriteByte('\n')
	return []byte(builder.String())
}

func sourceSpan(source []byte, span sfc.Span) string {
	start := span.Start.Offset
	end := span.End.Offset
	if start < 0 {
		start = 0
	}
	if end > len(source) {
		end = len(source)
	}
	if start > end {
		start = end
	}
	return string(source[start:end])
}

func formatTemplateContent(source string) string {
	source = trimBlockBoundaryNewlines(source)
	if strings.TrimSpace(source) == "" {
		return ""
	}

	tree, diagnostics := compilerTemplate.ParseBlock(&sfc.Block{Content: source})
	if len(diagnostics) != 0 {
		return source
	}
	protectedLines := templateTextLines(source, tree)

	lines := strings.Split(source, "\n")
	formatted := make([]string, 0, len(lines))
	indent := 1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(formatted) > 0 && formatted[len(formatted)-1] != "" {
				formatted = append(formatted, "")
			}
			continue
		}

		if protectedLines[index] {
			formatted = append(formatted, line)
			indent += templateLineIndentDelta(trimmed)
			if indent < 0 {
				indent = 0
			}
			continue
		}

		lineIndent := indent
		if strings.HasPrefix(trimmed, "</") && lineIndent > 0 {
			lineIndent--
		}
		formatted = append(formatted, strings.Repeat("\t", lineIndent)+trimmed)

		indent += templateLineIndentDelta(trimmed)
		if indent < 0 {
			indent = 0
		}
	}

	for len(formatted) > 0 && formatted[len(formatted)-1] == "" {
		formatted = formatted[:len(formatted)-1]
	}
	return strings.Join(formatted, "\n")
}

type templateContentLine struct {
	start int
	end   int
}

func templateTextLines(source string, tree *compilerTemplate.Tree) map[int]bool {
	lines := templateContentLines(source)
	protected := make(map[int]bool)
	var walk func(nodes []*compilerTemplate.Node)
	walk = func(nodes []*compilerTemplate.Node) {
		for _, node := range nodes {
			if node.Kind == compilerTemplate.NodeText {
				markTemplateTextLines(source, lines, protected, node.ContentSpan.Start.Offset, node.ContentSpan.End.Offset)
			}
			walk(node.Children)
		}
	}
	if tree != nil {
		walk(tree.Nodes)
	}
	return protected
}

func templateContentLines(source string) []templateContentLine {
	lines := make([]templateContentLine, 0, strings.Count(source, "\n")+1)
	start := 0
	for start <= len(source) {
		end := strings.IndexByte(source[start:], '\n')
		if end == -1 {
			lines = append(lines, templateContentLine{start: start, end: len(source)})
			break
		}
		end += start
		lines = append(lines, templateContentLine{start: start, end: end})
		start = end + 1
	}
	return lines
}

func markTemplateTextLines(source string, lines []templateContentLine, protected map[int]bool, start int, end int) {
	start, end = trimTemplateSpaceRange(source, start, end)
	if start == end {
		return
	}

	for index, line := range lines {
		if start < line.end && end > line.start {
			protected[index] = true
		}
	}
}

func trimTemplateSpaceRange(source string, start int, end int) (int, int) {
	if start < 0 {
		start = 0
	}
	if end > len(source) {
		end = len(source)
	}
	if start > end {
		start = end
	}
	for start < end && isTemplateSpace(source[start]) {
		start++
	}
	for end > start && isTemplateSpace(source[end-1]) {
		end--
	}
	return start, end
}

func templateLineIndentDelta(line string) int {
	delta := 0
	for offset := 0; offset < len(line); {
		if strings.HasPrefix(line[offset:], "<!--") {
			end := strings.Index(line[offset+len("<!--"):], "-->")
			if end == -1 {
				return delta
			}
			offset += len("<!--") + end + len("-->")
			continue
		}
		if line[offset] != '<' || offset+1 >= len(line) {
			offset++
			continue
		}

		if line[offset+1] == '/' {
			if end := templateTagEnd(line, offset); end != -1 {
				delta--
				offset = end
				continue
			}
		}
		if !isTemplateTagNameStart(line[offset+1]) {
			offset++
			continue
		}

		end := templateTagEnd(line, offset)
		if end == -1 {
			return delta
		}
		name := templateTagName(line, offset+1, end)
		if name != "" && !isVoidTemplateTag(name) && !isSelfClosingTemplateTag(line[offset:end]) {
			delta++
		}
		offset = end
	}
	return delta
}

func templateTagEnd(source string, start int) int {
	var quote byte
	for offset := start + 1; offset < len(source); offset++ {
		current := source[offset]
		if quote != 0 {
			if current == quote {
				quote = 0
			}
			continue
		}
		switch current {
		case '\'', '"':
			quote = current
		case '>':
			return offset + 1
		}
	}
	return -1
}

func templateTagName(source string, start int, end int) string {
	offset := start
	for offset < end && isTemplateTagNameChar(source[offset]) {
		offset++
	}
	return source[start:offset]
}

func isSelfClosingTemplateTag(tag string) bool {
	offset := len(tag) - 1
	if offset >= 0 && tag[offset] == '>' {
		offset--
	}
	for offset >= 0 && isTemplateSpace(tag[offset]) {
		offset--
	}
	return offset >= 0 && tag[offset] == '/'
}

func isVoidTemplateTag(name string) bool {
	switch strings.ToLower(name) {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	default:
		return false
	}
}

func isTemplateTagNameStart(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isTemplateTagNameChar(value byte) bool {
	return isTemplateTagNameStart(value) || value >= '0' && value <= '9' || value == '-' || value == '_' || value == ':'
}

func isTemplateSpace(value byte) bool {
	switch value {
	case ' ', '\n', '\r', '\t', '\f':
		return true
	default:
		return false
	}
}

func trimBlockBoundaryNewlines(source string) string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")
	return strings.Trim(source, "\n")
}
