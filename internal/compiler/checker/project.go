package checker

import (
	"fmt"

	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/sfc"
)

// CheckProject validates template expressions and component references across a
// parsed project.
func CheckProject(project Project) []Diagnostic {
	checker := projectChecker{
		components: make(map[string]componentBinding),
	}
	checker.indexComponents(project.Files)
	for _, file := range project.Files {
		checker.checkFile(file)
	}
	return checker.diagnostics
}

type projectChecker struct {
	components  map[string]componentBinding
	diagnostics []Diagnostic
}

type componentBinding struct {
	path      string
	component *script.Component
	props     map[string]script.Prop
}

func (c *projectChecker) indexComponents(files []File) {
	for _, file := range files {
		path := filePath(file)
		if file.Script == nil || file.Script.Component == nil {
			continue
		}

		component := file.Script.Component
		if previous, ok := c.components[component.Name]; ok {
			c.add(path, fmt.Sprintf("duplicate component %q; first declared in %s", component.Name, previous.path), component.NameSpan)
			continue
		}

		props := make(map[string]script.Prop, len(component.Props))
		for _, prop := range component.Props {
			props[prop.Name] = prop
		}
		c.components[component.Name] = componentBinding{
			path:      path,
			component: component,
			props:     props,
		}
	}
}

func (c *projectChecker) checkFile(file File) {
	path := filePath(file)
	if file.Template == nil {
		c.add(path, "missing parsed template", sfc.Span{})
		return
	}
	if file.Script == nil || file.Script.Component == nil {
		c.add(path, "missing component contract", file.Template.Span)
		return
	}

	fileChecker := fileChecker{
		path:       path,
		component:  file.Script.Component,
		components: c.components,
	}
	fileChecker.checkNodes(file.Template.Nodes, componentScope(file.Script.Component))
	c.diagnostics = append(c.diagnostics, fileChecker.diagnostics...)
}

func (c *projectChecker) add(path string, message string, span sfc.Span) {
	c.diagnostics = append(c.diagnostics, Diagnostic{
		Path:    path,
		Message: message,
		Span:    span,
	})
}

func filePath(file File) string {
	if file.Path != "" {
		return file.Path
	}
	if file.Script != nil && file.Script.Path != "" {
		return file.Script.Path
	}
	return ""
}
