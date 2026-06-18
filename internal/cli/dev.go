package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/norunners/tue/internal/compiler/checker"
	"github.com/norunners/tue/internal/compiler/gogen"
	"github.com/norunners/tue/internal/compiler/sfc"
)

const (
	defaultDevAddr = "127.0.0.1:5173"
	devClientPath  = "tue_dev.js"
	devEventsPath  = "/__tue/events"
)

type devOptions struct {
	Root         string
	Addr         string
	PollInterval time.Duration
}

type devEventType string

const (
	devEventTypeReady  devEventType = "ready"
	devEventTypeReload devEventType = "reload"
	devEventTypeStyle  devEventType = "style"
	devEventTypeError  devEventType = "error"
)

type devEvent struct {
	Type        devEventType    `json:"type"`
	Message     string          `json:"message,omitempty"`
	Diagnostics []devDiagnostic `json:"diagnostics,omitempty"`
}

type devDiagnostic struct {
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Message string `json:"message"`
}

func runDev(args []string, stdout, stderr io.Writer) int {
	options, code, ok := parseDevOptions(args, stdout, stderr)
	if !ok {
		return code
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := serveDev(ctx, options, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "tue dev: %v\n", err)
		return exitError
	}
	return exitOK
}

func parseDevOptions(args []string, stdout, stderr io.Writer) (devOptions, int, bool) {
	flags := flag.NewFlagSet("tue dev", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	options := devOptions{
		Addr:         defaultDevAddr,
		PollInterval: 500 * time.Millisecond,
	}
	flags.StringVar(&options.Addr, "addr", options.Addr, "address to listen on")
	flags.DurationVar(&options.PollInterval, "poll", options.PollInterval, "file polling interval")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printCommandUsage("dev", stdout)
			return devOptions{}, exitOK, false
		}
		fmt.Fprintf(stderr, "tue dev: %v\n\n", err)
		printCommandUsage("dev", stderr)
		return devOptions{}, exitUsage, false
	}
	if flags.NArg() > 1 {
		fmt.Fprint(stderr, "tue dev: expected at most one project root\n\n")
		printCommandUsage("dev", stderr)
		return devOptions{}, exitUsage, false
	}
	options.Root = "."
	if flags.NArg() == 1 {
		options.Root = flags.Arg(0)
	}
	if options.PollInterval <= 0 {
		fmt.Fprint(stderr, "tue dev: -poll must be greater than 0\n\n")
		printCommandUsage("dev", stderr)
		return devOptions{}, exitUsage, false
	}
	return options, exitOK, true
}

func serveDev(ctx context.Context, options devOptions, stdout, stderr io.Writer) error {
	root := filepath.Clean(options.Root)
	if _, err := os.Stat(root); err != nil {
		return fmt.Errorf("stat project root %q: %w", root, err)
	}

	listener, err := net.Listen("tcp", options.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", options.Addr, err)
	}
	defer listener.Close()

	broadcaster := newDevBroadcaster()
	initialEvent := buildDevProject(root, devEventTypeReady)
	broadcaster.publish(initialEvent)
	printDevEvent(stderr, initialEvent)

	snapshot, err := captureDevSnapshot(root)
	if err != nil {
		event := devErrorEvent(fmt.Sprintf("watch project files: %v", err), nil)
		broadcaster.publish(event)
		printDevEvent(stderr, event)
	}

	mux := http.NewServeMux()
	mux.HandleFunc(devEventsPath, broadcaster.serveEvents)
	mux.Handle("/", http.FileServer(http.Dir(filepath.Join(root, gogen.DistDir))))
	server := &http.Server{Handler: mux}

	go watchDevProject(ctx, root, options.PollInterval, snapshot, broadcaster, stderr)

	fmt.Fprintf(stdout, "tue dev: serving %s at http://%s\n", root, listener.Addr().String())

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func watchDevProject(ctx context.Context, root string, interval time.Duration, snapshot devSnapshot, broadcaster *devBroadcaster, stderr io.Writer) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		next, err := captureDevSnapshot(root)
		if err != nil {
			event := devErrorEvent(fmt.Sprintf("watch project files: %v", err), nil)
			broadcaster.publish(event)
			printDevEvent(stderr, event)
			continue
		}

		changes := diffDevSnapshots(snapshot, next)
		if len(changes) == 0 {
			continue
		}
		snapshot = next

		event := buildDevProject(root, classifyDevEventType(changes))
		broadcaster.publish(event)
		printDevEvent(stderr, event)
	}
}

func buildDevProject(root string, successType devEventType) devEvent {
	event := compileDevProject(root, successType)
	if event.Type == devEventTypeError {
		if err := writeDevErrorPage(root, event); err != nil {
			return devErrorEvent(fmt.Sprintf("write dev error page: %v", err), nil)
		}
		return event
	}
	if err := writeDevClientFiles(root); err != nil {
		return devErrorEvent(fmt.Sprintf("write dev client: %v", err), nil)
	}
	return event
}

func compileDevProject(root string, successType devEventType) devEvent {
	files, err := discoverTueFiles(root)
	if err != nil {
		return devErrorEvent(err.Error(), nil)
	}

	parsedFiles, parseDiagnostics, err := parseParsedTueFiles(root, files)
	if err != nil {
		return devErrorEvent(err.Error(), nil)
	}
	if len(parseDiagnostics) != 0 {
		return devErrorEvent("Tue diagnostics", devDiagnosticsFromChecker(parseDiagnostics))
	}

	checkFiles := make([]checker.File, len(parsedFiles))
	gogenFiles := make([]gogen.File, len(parsedFiles))
	for i, file := range parsedFiles {
		checkFiles[i] = file.CheckerFile
		gogenFiles[i] = file.gogenFile()
	}

	checkDiagnostics := checker.CheckProject(checker.Project{Files: checkFiles})
	if len(checkDiagnostics) != 0 {
		return devErrorEvent("Tue diagnostics", devDiagnosticsFromChecker(checkDiagnostics))
	}

	build, buildDiagnostics, err := gogen.WriteProductionProject(root, gogen.Project{
		Root:  root,
		Files: gogenFiles,
	})
	if err != nil {
		return devErrorEvent(err.Error(), nil)
	}
	if len(buildDiagnostics) != 0 {
		return devErrorEvent("Tue generation diagnostics", devDiagnosticsFromChecker(gogenDiagnosticsFor(buildDiagnostics)))
	}
	return devEvent{
		Type:    successType,
		Message: fmt.Sprintf("generated %d component(s)", len(build.Manifest.Files)),
	}
}

func devErrorEvent(message string, diagnostics []devDiagnostic) devEvent {
	return devEvent{
		Type:        devEventTypeError,
		Message:     message,
		Diagnostics: diagnostics,
	}
}

func devDiagnosticsFromChecker(diagnostics []checker.Diagnostic) []devDiagnostic {
	converted := make([]devDiagnostic, len(diagnostics))
	for i, diagnostic := range diagnostics {
		converted[i] = devDiagnostic{
			Path:    diagnostic.Path,
			Line:    diagnostic.Span.Start.Line,
			Column:  diagnostic.Span.Start.Column,
			Message: diagnostic.Message,
		}
	}
	return converted
}

func writeDevClientFiles(root string) error {
	distDir := filepath.Join(root, gogen.DistDir)
	indexPath := filepath.Join(distDir, "index.html")
	indexSource, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("read dev index: %w", err)
	}
	if err := os.WriteFile(indexPath, injectDevClient(indexSource), 0o644); err != nil {
		return fmt.Errorf("write dev index: %w", err)
	}
	if err := os.WriteFile(filepath.Join(distDir, devClientPath), devClientSource(), 0o644); err != nil {
		return fmt.Errorf("write dev client: %w", err)
	}
	return nil
}

func writeDevErrorPage(root string, event devEvent) error {
	distDir := filepath.Join(root, gogen.DistDir)
	if err := os.RemoveAll(distDir); err != nil {
		return fmt.Errorf("clean dev dist: %w", err)
	}
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return fmt.Errorf("create dev dist: %w", err)
	}
	index := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Tue Dev Error</title>
	<script src="/%s" defer></script>
</head>
<body>
	<div id="app"></div>
	<pre>%s</pre>
</body>
</html>
`, devClientPath, html.EscapeString(event.Message))
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte(index), 0o644); err != nil {
		return fmt.Errorf("write dev error index: %w", err)
	}
	if err := os.WriteFile(filepath.Join(distDir, devClientPath), devClientSource(), 0o644); err != nil {
		return fmt.Errorf("write dev client: %w", err)
	}
	return nil
}

func injectDevClient(source []byte) []byte {
	const script = "\t<script src=\"/tue_dev.js\" defer></script>\n"
	text := string(source)
	if strings.Contains(text, "/"+devClientPath) {
		return source
	}
	index := strings.LastIndex(strings.ToLower(text), "</body>")
	if index == -1 {
		return []byte(text + "\n" + script)
	}
	return []byte(text[:index] + script + text[index:])
}

func devClientSource() []byte {
	return []byte(`(function () {
	"use strict";

	let overlay;

	function ensureOverlay() {
		if (overlay) {
			return overlay;
		}
		overlay = document.createElement("div");
		overlay.id = "tue-error-overlay";
		overlay.style.cssText = "position:fixed;inset:0;z-index:2147483647;background:rgba(20,24,31,.96);color:#f8fafc;font:14px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;padding:24px;overflow:auto;white-space:pre-wrap;";
		document.body.appendChild(overlay);
		return overlay;
	}

	function hideOverlay() {
		if (overlay) {
			overlay.remove();
			overlay = null;
		}
	}

	function formatDiagnostic(diagnostic) {
		const location = diagnostic.path ? diagnostic.path + (diagnostic.line ? ":" + diagnostic.line + ":" + diagnostic.column : "") + ": " : "";
		return location + diagnostic.message;
	}

	function showError(event) {
		const lines = ["Tue dev error", ""];
		if (event.message) {
			lines.push(event.message, "");
		}
		if (event.diagnostics && event.diagnostics.length) {
			lines.push(...event.diagnostics.map(formatDiagnostic));
		}
		ensureOverlay().textContent = lines.join("\n");
	}

	function reloadStyles() {
		const timestamp = Date.now();
		for (const link of document.querySelectorAll('link[rel="stylesheet"]')) {
			const url = new URL(link.href, window.location.href);
			url.searchParams.set("tue", String(timestamp));
			link.href = url.toString();
		}
	}

	function handleEvent(event) {
		if (!event || event.type === "ready") {
			hideOverlay();
			return;
		}
		if (event.type === "style") {
			hideOverlay();
			reloadStyles();
			return;
		}
		if (event.type === "reload") {
			hideOverlay();
			window.location.reload();
			return;
		}
		if (event.type === "error") {
			showError(event);
		}
	}

	const source = new EventSource("/__tue/events");
	source.onmessage = function (message) {
		handleEvent(JSON.parse(message.data));
	};
	source.onerror = function () {
		showError({ type: "error", message: "Lost connection to Tue dev server." });
	};

	window.addEventListener("error", function (event) {
		showError({ type: "error", message: event.message || "Runtime error" });
	});
	window.addEventListener("unhandledrejection", function (event) {
		showError({ type: "error", message: String(event.reason || "Unhandled promise rejection") });
	});
})();
`)
}

func printDevEvent(stderr io.Writer, event devEvent) {
	if event.Type == devEventTypeError {
		fmt.Fprintf(stderr, "tue dev: %s\n", event.Message)
		for _, diagnostic := range event.Diagnostics {
			if diagnostic.Path != "" && diagnostic.Line > 0 && diagnostic.Column > 0 {
				fmt.Fprintf(stderr, "%s:%d:%d: %s\n", diagnostic.Path, diagnostic.Line, diagnostic.Column, diagnostic.Message)
				continue
			}
			if diagnostic.Path != "" {
				fmt.Fprintf(stderr, "%s: %s\n", diagnostic.Path, diagnostic.Message)
				continue
			}
			fmt.Fprintf(stderr, "%s\n", diagnostic.Message)
		}
		return
	}
	fmt.Fprintf(stderr, "tue dev: %s\n", event.Message)
}

type devBroadcaster struct {
	mu      sync.Mutex
	current devEvent
	clients map[chan devEvent]struct{}
}

func newDevBroadcaster() *devBroadcaster {
	return &devBroadcaster{
		current: devEvent{Type: devEventTypeReady},
		clients: make(map[chan devEvent]struct{}),
	}
}

func (b *devBroadcaster) publish(event devEvent) {
	b.mu.Lock()
	b.current = event
	clients := make([]chan devEvent, 0, len(b.clients))
	for client := range b.clients {
		clients = append(clients, client)
	}
	b.mu.Unlock()

	for _, client := range clients {
		select {
		case client <- event:
		default:
		}
	}
}

func (b *devBroadcaster) subscribe() (chan devEvent, devEvent) {
	client := make(chan devEvent, 8)
	b.mu.Lock()
	current := b.current
	b.clients[client] = struct{}{}
	b.mu.Unlock()
	return client, current
}

func (b *devBroadcaster) unsubscribe(client chan devEvent) {
	b.mu.Lock()
	delete(b.clients, client)
	b.mu.Unlock()
}

func (b *devBroadcaster) serveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	client, current := b.subscribe()
	defer b.unsubscribe(client)

	writeDevSSE(w, current)
	flusher.Flush()

	for {
		select {
		case event := <-client:
			writeDevSSE(w, event)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeDevSSE(w io.Writer, event devEvent) {
	source, err := json.Marshal(event)
	if err != nil {
		source, _ = json.Marshal(devErrorEvent(fmt.Sprintf("encode dev event: %v", err), nil))
	}
	fmt.Fprintf(w, "data: %s\n\n", source)
}

type devSnapshot map[string]devWatchedFile

type devWatchedFile struct {
	Hash   [32]byte
	Source []byte
}

type devFileChange struct {
	Path   string
	Before *devWatchedFile
	After  *devWatchedFile
}

func captureDevSnapshot(root string) (devSnapshot, error) {
	snapshot := make(devSnapshot)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && shouldSkipDevDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(relativePath)] = devWatchedFile{
			Hash:   sha256.Sum256(source),
			Source: source,
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("capture file snapshot: %w", err)
	}
	return snapshot, nil
}

func shouldSkipDevDir(name string) bool {
	return shouldSkipDir(name) || name == gogen.DistDir
}

func diffDevSnapshots(before devSnapshot, after devSnapshot) []devFileChange {
	changes := make([]devFileChange, 0)
	for path, next := range after {
		previous, ok := before[path]
		if ok && previous.Hash == next.Hash {
			continue
		}
		change := devFileChange{Path: path, After: &next}
		if ok {
			change.Before = &previous
		}
		changes = append(changes, change)
	}
	for path, previous := range before {
		if _, ok := after[path]; ok {
			continue
		}
		change := devFileChange{Path: path, Before: &previous}
		changes = append(changes, change)
	}
	return changes
}

func classifyDevEventType(changes []devFileChange) devEventType {
	if len(changes) == 0 {
		return devEventTypeReload
	}
	for _, change := range changes {
		if !isStyleOnlyDevChange(change) {
			return devEventTypeReload
		}
	}
	return devEventTypeStyle
}

func isStyleOnlyDevChange(change devFileChange) bool {
	if change.Before == nil || change.After == nil {
		return false
	}
	switch filepath.Ext(change.Path) {
	case ".css":
		return true
	case ".tue":
		return tueStyleOnlyChange(change.Before.Source, change.After.Source)
	default:
		return false
	}
}

func tueStyleOnlyChange(before []byte, after []byte) bool {
	beforeFile, beforeDiagnostics := sfc.Parse("style-check.tue", before)
	afterFile, afterDiagnostics := sfc.Parse("style-check.tue", after)
	if len(beforeDiagnostics) != 0 || len(afterDiagnostics) != 0 {
		return false
	}
	return nonStyleSignature(beforeFile) == nonStyleSignature(afterFile)
}

func nonStyleSignature(file *sfc.File) string {
	var builder strings.Builder
	for _, block := range file.Blocks {
		if block.Kind == sfc.BlockStyle {
			continue
		}
		builder.WriteString(string(block.Kind))
		builder.WriteByte('\n')
		builder.WriteString(block.Name)
		builder.WriteByte('\n')
		for _, attr := range block.Attrs {
			builder.WriteString(attr.Name)
			builder.WriteByte('=')
			builder.WriteString(attr.Value)
			builder.WriteByte('\n')
		}
		builder.WriteString(block.Content)
		builder.WriteByte('\n')
	}
	return builder.String()
}
