package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// manifest is the content of exercises/modules.json. It is the single source
// of truth for module order, titles, parts and whether a module is enabled.
type manifest struct {
	Parts   []manifestPart   `json:"parts"`
	Modules []manifestModule `json:"modules"`
}

type manifestPart struct {
	ID string `json:"id"`
	En string `json:"en"`
	Es string `json:"es"`
}

type manifestModule struct {
	Slug    string       `json:"slug"`
	Part    string       `json:"part"`
	Enabled bool         `json:"enabled"`
	En      manifestText `json:"en"`
	Es      manifestText `json:"es"`
}

type manifestText struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

func loadManifest(path string) (*manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading modules manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing modules manifest %s: %w", path, err)
	}
	return &m, nil
}

// matches reports whether a user-provided selector ("03", "03-preallocation")
// refers to the module.
func (mod manifestModule) matches(selector string) bool {
	return mod.Slug == selector || strings.HasPrefix(mod.Slug, selector+"-")
}

// selectModules returns the modules to build. With an explicit only list the
// manifest's enabled flags are ignored; skip always removes modules.
func (m *manifest) selectModules(only, skip []string) ([]manifestModule, error) {
	for _, sel := range append(append([]string{}, only...), skip...) {
		found := false
		for _, mod := range m.Modules {
			if mod.matches(sel) {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown module %q", sel)
		}
	}

	anyMatch := func(mod manifestModule, sels []string) bool {
		for _, sel := range sels {
			if mod.matches(sel) {
				return true
			}
		}
		return false
	}

	var out []manifestModule
	for _, mod := range m.Modules {
		include := mod.Enabled
		if len(only) > 0 {
			include = anyMatch(mod, only)
		}
		if include && !anyMatch(mod, skip) {
			out = append(out, mod)
		}
	}
	return out, nil
}

func (m *manifest) metadataFor(lang string, modules []manifestModule) ([]exerciseMeta, error) {
	partTitles := make(map[string]string, len(m.Parts))
	for _, p := range m.Parts {
		if lang == "es" {
			partTitles[p.ID] = p.Es
		} else {
			partTitles[p.ID] = p.En
		}
	}

	metas := make([]exerciseMeta, 0, len(modules))
	for _, mod := range modules {
		num, err := strconv.Atoi(strings.SplitN(mod.Slug, "-", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("module %q: slug must start with a number: %w", mod.Slug, err)
		}
		text := mod.En
		if lang == "es" {
			text = mod.Es
		}
		part, ok := partTitles[mod.Part]
		if !ok {
			return nil, fmt.Errorf("module %q: unknown part %q", mod.Slug, mod.Part)
		}
		metas = append(metas, exerciseMeta{
			Filename:    mod.Slug,
			Number:      num,
			Title:       text.Title,
			Description: text.Description,
			PartTitle:   part,
		})
	}
	return metas, nil
}
