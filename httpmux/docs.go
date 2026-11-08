package httpmux

import (
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
)

//go:embed docs/swagger.json docs/swagger.yaml
var specFiles embed.FS

type spec struct {
	Info struct {
		Description string `json:"description"`
	} `json:"info"`
	BasePath    string                          `json:"basePath"`
	Paths       map[string]map[string]operation `json:"paths"`
	Definitions map[string]struct {
		Properties map[string]property `json:"properties"`
	} `json:"definitions"`
}

type operation struct {
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Parameters  []struct {
		Name        string `json:"name"`
		In          string `json:"in"`
		Description string `json:"description"`
		Required    bool   `json:"required"`
	} `json:"parameters"`
	Responses map[string]struct {
		Description string `json:"description"`
		Schema      *ref   `json:"schema"`
	} `json:"responses"`
	Security []map[string][]string `json:"security"`
}

type ref struct {
	Ref string `json:"$ref"`
}

type property struct {
	Type    string `json:"type"`
	Format  string `json:"format"`
	Example any    `json:"example"`
}

type endpoint struct {
	Method, Path, Anchor, Summary, Description string
	Secured                                    bool
	Parameters                                 any
	Responses                                  []response
}

type response struct {
	Status, Schema, Description string
}

type model struct {
	Name   string
	Fields []field
}

type field struct {
	Name, Type, Example string
}

type docsPage struct {
	Spec      spec
	Endpoints []endpoint
	Models    []model
}

func modelName(r *ref) string {
	if r == nil {
		return ""
	}
	name := r.Ref[strings.LastIndex(r.Ref, "/")+1:]
	return strings.TrimPrefix(name, "api.")
}

func loadDocs(overrides map[string]map[string]string) (docsPage, error) {
	var page docsPage
	raw, err := specFiles.ReadFile("docs/swagger.json")
	if err != nil {
		return page, err
	}
	if err := json.Unmarshal(raw, &page.Spec); err != nil {
		return page, fmt.Errorf("swagger.json: %w", err)
	}

	for _, path := range slices.Sorted(maps.Keys(page.Spec.Paths)) {
		for _, method := range []string{"get", "post", "put", "delete"} {
			op, ok := page.Spec.Paths[path][method]
			if !ok {
				continue
			}
			e := endpoint{
				Method:      strings.ToUpper(method),
				Path:        page.Spec.BasePath + path,
				Anchor:      method + strings.NewReplacer("/", "-", "{", "", "}", "").Replace(path),
				Summary:     op.Summary,
				Description: op.Description,
				Secured:     len(op.Security) > 0,
				Parameters:  op.Parameters,
			}
			for _, status := range slices.Sorted(maps.Keys(op.Responses)) {
				r := op.Responses[status]
				e.Responses = append(e.Responses, response{Status: status, Schema: modelName(r.Schema), Description: r.Description})
			}
			page.Endpoints = append(page.Endpoints, e)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(page.Spec.Definitions)) {
		m := model{Name: strings.TrimPrefix(name, "api.")}
		for _, prop := range slices.Sorted(maps.Keys(page.Spec.Definitions[name].Properties)) {
			p := page.Spec.Definitions[name].Properties[prop]
			example := overrides[m.Name][prop]
			if example == "" && p.Example != nil {
				example = fmt.Sprint(p.Example)
			}
			m.Fields = append(m.Fields, field{Name: prop, Type: cmp.Or(p.Format, p.Type), Example: example})
		}
		page.Models = append(page.Models, m)
	}
	return page, nil
}

func (m *Mux) docsPage(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	logger = logger.WithGroup("docsPage")
	m.render(logger, w, http.StatusOK, "docs", map[string]any{
		"Site":          m.site(),
		"Spec":          m.docs.Spec,
		"Endpoints":     m.docs.Endpoints,
		"Models":        m.docs.Models,
		"MinWait":       m.cfg.ChallengeMinWait.Duration,
		"Lifetime":      m.cfg.ChallengeLifetime.Duration,
		"Difficulty":    m.cfg.PowDifficulty,
		"TokenValidity": m.cfg.TokenValidity.Duration,
		"RateLimit":     m.cfg.RateLimit.Duration,
	})
}

func (m *Mux) spec(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	contentType := map[string]string{"swagger.json": "application/json", "swagger.yaml": "application/yaml"}[name]
	if contentType == "" {
		http.NotFound(w, r)
		return
	}
	data, err := specFiles.ReadFile("docs/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}
