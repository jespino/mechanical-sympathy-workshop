package main

import (
	_ "embed"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/russross/blackfriday/v2"
)

//go:embed static/styles.css
var stylesCSS []byte

//go:embed static/app.js
var appJS []byte

type Exercise struct {
	Number         int
	Title          string
	Description    string
	Filename       string
	Slug           string
	Content        template.HTML
	PrevLink       string
	NextLink       string
	PrevTitle      string
	NextTitle      string
	PartTitle      string
	Lang           string
	LangName       string
	AllSlugs       string
	AltLangURL     string
	AltLangName    string
	CSSPath        string
	AppJSPath      string
	HomePath       string
	TotalExercises int
}

// PartGroup is a run of consecutive exercises that belong to the same part,
// used to render the index page in sections.
type PartGroup struct {
	Title     string
	Exercises []Exercise
}

type IndexData struct {
	Exercises   []Exercise
	Parts       []PartGroup
	Lang        string
	LangName    string
	AllSlugs    string
	AltLangURL  string
	AltLangName string
	CSSPath     string
	AppJSPath   string
	HomePath    string
}

type exerciseMeta struct {
	Filename    string
	Number      int
	Title       string
	Description string
	PartTitle   string
}

type LangConfig struct {
	Code          string
	Name          string
	FileSuffix    string
	OutputPrefix  string
	AltLangPrefix string
	AltLangName   string
	UIStrings     UIStrings
}

type UIStrings struct {
	Home                string
	Previous            string
	Next                string
	Exercise            string
	HeroTitle           string
	HeroLead            string
	HeroVersionNote     string
	Prerequisites       string
	PrereqItems         []string
	Overview            string
	OverviewText        string
	GettingStarted      string
	GettingStartedItems []string
	CTAButton           string
	FooterTitle         string
	FooterCreatedBy     string
}

var englishConfig = LangConfig{
	Code:          "en",
	Name:          "English",
	FileSuffix:    ".md",
	OutputPrefix:  "",
	AltLangPrefix: "es",
	AltLangName:   "Español",
	UIStrings: UIStrings{
		Home:            "Home",
		Previous:        "Previous",
		Next:            "Next",
		Exercise:        "Module",
		HeroTitle:       "Go Mechanical Sympathy Workshop",
		HeroLead:        "A hands-on workshop about writing Go that works with the machine instead of against it. Every module explains a mechanism inside the Go compiler, runtime or the hardware, and then you measure the before and after yourself with benchmarks and profiles.",
		HeroVersionNote: "<strong>This workshop uses stock Go 1.27</strong>. You don't need to build Go from source.",
		Prerequisites:   "Prerequisites",
		PrereqItems: []string{
			"Solid knowledge of Go programming",
			"Familiarity with command line tools",
			"<strong>Go 1.27 or newer</strong> installed",
			"<code>benchstat</code>: <code>go install golang.org/x/perf/cmd/benchstat@latest</code>",
			"Graphviz (optional, for <code>pprof</code> graph views)",
			"<strong>Linux with <code>perf</code></strong> (optional, used for hardware counters in Part III)",
		},
		Overview:       "Workshop Overview",
		OverviewText:   "This workshop has %d modules that build on each other. You start by learning to measure, then remove allocations, then tune data for the CPU caches and branch predictor, and then work with the GC and scheduler. Every module pairs one concept with a lab where you measure the before and the after.",
		GettingStarted: "Getting Started",
		GettingStartedItems: []string{
			`Start with <a href="%s00-introduction-setup.html">Module 0</a> to set up your environment`,
			"Do modules 1 and 2 first. Every other module relies on benchmarking and profiling",
			"Each lab lives in <code>labs/NN-name/</code>. You edit <code>after.go</code>, and <code>solution.go</code> is the reference",
		},
		CTAButton:       "Start with Module 0 →",
		FooterTitle:     "Go Mechanical Sympathy Workshop",
		FooterCreatedBy: "Created by <strong>Jesús Espino</strong>",
	},
}

var spanishConfig = LangConfig{
	Code:          "es",
	Name:          "Español",
	FileSuffix:    ".es.md",
	OutputPrefix:  "es",
	AltLangPrefix: "",
	AltLangName:   "English",
	UIStrings: UIStrings{
		Home:            "Inicio",
		Previous:        "Anterior",
		Next:            "Siguiente",
		Exercise:        "Módulo",
		HeroTitle:       "Workshop de Simpatía Mecánica en Go",
		HeroLead:        "Un workshop práctico sobre cómo escribir Go que trabaje con la máquina y no contra ella. Cada módulo explica un mecanismo del compilador, del runtime de Go o del hardware, y luego mides tú mismo el antes y el después con benchmarks y perfiles.",
		HeroVersionNote: "<strong>Este workshop usa Go 1.27 estándar</strong>. No necesitas compilar Go desde el código fuente.",
		Prerequisites:   "Requisitos Previos",
		PrereqItems: []string{
			"Conocimiento sólido de programación en Go",
			"Familiaridad con herramientas de línea de comandos",
			"<strong>Go 1.27 o superior</strong> instalado",
			"<code>benchstat</code>: <code>go install golang.org/x/perf/cmd/benchstat@latest</code>",
			"Graphviz (opcional, para las vistas de grafo de <code>pprof</code>)",
			"<strong>Linux con <code>perf</code></strong> (opcional, para los contadores hardware de la Parte III)",
		},
		Overview:       "Descripción General",
		OverviewText:   "Este workshop tiene %d módulos que se apoyan unos en otros. Empiezas aprendiendo a medir, después eliminas asignaciones, luego adaptas los datos a las cachés y al predictor de saltos, y por último trabajas con el GC y el scheduler. Cada módulo combina un concepto con un laboratorio donde mides el antes y el después.",
		GettingStarted: "Cómo Empezar",
		GettingStartedItems: []string{
			`Empieza con el <a href="%s00-introduction-setup.html">Módulo 0</a> para configurar tu entorno`,
			"Haz primero los módulos 1 y 2. Todos los demás se apoyan en benchmarks y profiling",
			"Cada laboratorio vive en <code>labs/NN-nombre/</code>. Tú editas <code>after.go</code>, y <code>solution.go</code> es la referencia",
		},
		CTAButton:       "Comenzar con el Módulo 0 →",
		FooterTitle:     "Workshop de Simpatía Mecánica en Go",
		FooterCreatedBy: "Creado por <strong>Jesús Espino</strong>",
	},
}

var languages = []LangConfig{englishConfig, spanishConfig}

// siteOptions holds everything needed to build the site. It is shared by the
// one-shot generator and the dev server.
type siteOptions struct {
	exercisesDir string
	imagesDir    string
	outputDir    string
	manifestPath string
	only         []string
	skip         []string
}

func main() {
	exercisesDir := flag.String("exercises", "../exercises", "Path to exercises directory")
	imagesDir := flag.String("images", "../images", "Path to images directory")
	outputDir := flag.String("output", "../website", "Path to output directory")
	manifest := flag.String("modules", "", "Path to the modules manifest (default: <exercises>/modules.json)")
	only := flag.String("only", "", "Comma-separated modules to include, ignoring 'enabled' in the manifest (e.g. 00,01,03-preallocation)")
	skip := flag.String("skip", "", "Comma-separated modules to exclude (e.g. 12,17)")
	serve := flag.Bool("serve", false, "Start dev server with live reload")
	port := flag.Int("port", 8888, "Dev server port (used with -serve)")
	flag.Parse()

	opts := siteOptions{
		exercisesDir: *exercisesDir,
		imagesDir:    *imagesDir,
		outputDir:    *outputDir,
		manifestPath: *manifest,
		only:         splitList(*only),
		skip:         splitList(*skip),
	}
	if opts.manifestPath == "" {
		opts.manifestPath = filepath.Join(opts.exercisesDir, "modules.json")
	}

	if *serve {
		srv := newDevServer(opts, *port)
		if err := srv.run(); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	totalPages, err := buildSite(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Website generated successfully!")
	fmt.Printf("📁 Output directory: %s\n", opts.outputDir)
	fmt.Printf("📄 Generated %d pages total (including all languages)\n", totalPages)
}

func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// buildSite renders every enabled module in every language, plus the index
// pages and static assets. It returns the number of HTML pages written.
func buildSite(opts siteOptions) (int, error) {
	m, err := loadManifest(opts.manifestPath)
	if err != nil {
		return 0, err
	}
	selected, err := m.selectModules(opts.only, opts.skip)
	if err != nil {
		return 0, err
	}
	if len(selected) == 0 {
		return 0, fmt.Errorf("no modules enabled")
	}

	enabled := make(map[string]bool, len(selected))
	for _, mod := range selected {
		enabled[mod.Slug] = true
	}

	if err := os.MkdirAll(opts.outputDir, 0o755); err != nil {
		return 0, fmt.Errorf("creating output directory: %w", err)
	}

	totalPages := 0
	for _, lang := range languages {
		langOutputDir := opts.outputDir
		if lang.OutputPrefix != "" {
			langOutputDir = filepath.Join(opts.outputDir, lang.OutputPrefix)
			if err := os.MkdirAll(langOutputDir, 0o755); err != nil {
				return 0, fmt.Errorf("creating output directory for %s: %w", lang.Code, err)
			}
		}
		if err := removeStalePages(langOutputDir, enabled); err != nil {
			return 0, err
		}

		cssPath := "styles.css"
		appJSPath := "app.js"
		if lang.OutputPrefix != "" {
			cssPath = "../styles.css"
			appJSPath = "../app.js"
		}

		homePath := ""
		altLangURLPrefix := "../"
		if lang.OutputPrefix == "" {
			altLangURLPrefix = "es/"
		}

		metas, err := m.metadataFor(lang.Code, selected)
		if err != nil {
			return 0, err
		}

		exercises := make([]Exercise, 0, len(metas))
		for i := range metas {
			exercise, err := generateExercisePage(opts.exercisesDir, langOutputDir, lang, metas, i, enabled, cssPath, appJSPath, homePath, altLangURLPrefix)
			if err != nil {
				return 0, fmt.Errorf("generating exercise %s (%s): %w", metas[i].Filename, lang.Code, err)
			}
			exercises = append(exercises, exercise)
		}

		if err := generateIndexPage(langOutputDir, lang, exercises, cssPath, appJSPath, homePath, altLangURLPrefix); err != nil {
			return 0, fmt.Errorf("generating index page (%s): %w", lang.Code, err)
		}

		totalPages += len(exercises) + 1
	}

	if err := writeStaticFile(opts.outputDir, "styles.css", stylesCSS); err != nil {
		return 0, err
	}
	if err := writeStaticFile(opts.outputDir, "app.js", appJS); err != nil {
		return 0, err
	}
	if err := copyImages(opts.imagesDir, filepath.Join(opts.outputDir, "images")); err != nil {
		return 0, fmt.Errorf("copying images: %w", err)
	}

	return totalPages, nil
}

func generateExercisePage(exercisesDir, outputDir string, lang LangConfig, metas []exerciseMeta, index int, enabled map[string]bool, cssPath, appJSPath, homePath, altLangURLPrefix string) (Exercise, error) {
	meta := metas[index]
	mdFilename := meta.Filename + lang.FileSuffix
	mdPath := filepath.Join(exercisesDir, mdFilename)
	content, err := os.ReadFile(mdPath)
	if err != nil {
		return Exercise{}, fmt.Errorf("reading markdown file: %w", err)
	}

	htmlContent := markdownToHTML(content, enabled)
	// The module description goes right under the page's own H1.
	htmlContent = strings.Replace(htmlContent, "</h1>", "</h1>\n<p class=\"module-lede\">"+template.HTMLEscapeString(meta.Description)+"</p>", 1)
	// Make image paths language-aware using the same prefix scheme as the
	// CSS/JS assets ("" at the site root, "../" inside a language subdir).
	if assetPrefix := strings.TrimSuffix(cssPath, "styles.css"); assetPrefix != "" {
		htmlContent = strings.ReplaceAll(htmlContent, `src="images/`, `src="`+assetPrefix+`images/`)
	}
	htmlFilename := meta.Filename + ".html"
	slug := meta.Filename

	prevLink := homePath + "index.html"
	prevTitle := lang.UIStrings.Home
	if index > 0 {
		prevLink = metas[index-1].Filename + ".html"
		prevTitle = metas[index-1].Title
	}

	nextLink := ""
	nextTitle := ""
	if index < len(metas)-1 {
		nextLink = metas[index+1].Filename + ".html"
		nextTitle = metas[index+1].Title
	}

	altLangURL := altLangURLPrefix + htmlFilename

	exercise := Exercise{
		Number:         meta.Number,
		Title:          meta.Title,
		Description:    meta.Description,
		Filename:       htmlFilename,
		Slug:           slug,
		Content:        template.HTML(htmlContent),
		PrevLink:       prevLink,
		NextLink:       nextLink,
		PrevTitle:      prevTitle,
		NextTitle:      nextTitle,
		PartTitle:      meta.PartTitle,
		Lang:           lang.Code,
		LangName:       lang.Name,
		AllSlugs:       allSlugs(metas),
		AltLangURL:     altLangURL,
		AltLangName:    lang.AltLangName,
		CSSPath:        cssPath,
		AppJSPath:      appJSPath,
		HomePath:       homePath,
		TotalExercises: len(metas),
	}

	tmpl, err := template.New("exercise").Funcs(template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"add":      func(a, b int) int { return a + b },
	}).Parse(exerciseTemplate)
	if err != nil {
		return Exercise{}, fmt.Errorf("parsing template: %w", err)
	}

	outputPath := filepath.Join(outputDir, htmlFilename)
	f, err := os.Create(outputPath)
	if err != nil {
		return Exercise{}, fmt.Errorf("creating output file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, exercise); err != nil {
		return Exercise{}, fmt.Errorf("executing template: %w", err)
	}

	fmt.Printf("✓ Generated %s [%s]\n", htmlFilename, lang.Code)
	return exercise, nil
}

func generateIndexPage(outputDir string, lang LangConfig, exercises []Exercise, cssPath, appJSPath, homePath, altLangURLPrefix string) error {
	tmpl, err := template.New("index").Funcs(template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	}).Parse(indexTemplate)
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}

	outputPath := filepath.Join(outputDir, "index.html")
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer f.Close()

	ui := lang.UIStrings
	ui.OverviewText = fmt.Sprintf(ui.OverviewText, len(exercises))

	formattedGSItems := make([]string, len(ui.GettingStartedItems))
	for i, item := range ui.GettingStartedItems {
		if strings.Contains(item, "%s") {
			formattedGSItems[i] = fmt.Sprintf(item, "")
		} else {
			formattedGSItems[i] = item
		}
	}
	ui.GettingStartedItems = formattedGSItems

	var parts []PartGroup
	for _, ex := range exercises {
		if len(parts) == 0 || parts[len(parts)-1].Title != ex.PartTitle {
			parts = append(parts, PartGroup{Title: ex.PartTitle})
		}
		parts[len(parts)-1].Exercises = append(parts[len(parts)-1].Exercises, ex)
	}

	data := struct {
		IndexData
		UI              UIStrings
		AltLangURLIndex string
	}{
		IndexData: IndexData{
			Exercises:   exercises,
			Parts:       parts,
			Lang:        lang.Code,
			LangName:    lang.Name,
			AllSlugs:    allSlugsOf(exercises),
			AltLangURL:  altLangURLPrefix + "index.html",
			AltLangName: lang.AltLangName,
			CSSPath:     cssPath,
			AppJSPath:   appJSPath,
			HomePath:    homePath,
		},
		UI:              ui,
		AltLangURLIndex: altLangURLPrefix + "index.html",
	}

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	fmt.Printf("✓ Generated index.html [%s]\n", lang.Code)
	return nil
}

// allSlugs lists the slugs in this build, for the progress counter.
func allSlugs(metas []exerciseMeta) string {
	slugs := make([]string, len(metas))
	for i, m := range metas {
		slugs[i] = m.Filename
	}
	return strings.Join(slugs, ",")
}

func allSlugsOf(exercises []Exercise) string {
	slugs := make([]string, len(exercises))
	for i, e := range exercises {
		slugs[i] = e.Slug
	}
	return strings.Join(slugs, ",")
}

// removeStalePages deletes module pages left over from a previous build in
// which a now-disabled module was still enabled.
func removeStalePages(dir string, enabled map[string]bool) error {
	pages, err := filepath.Glob(filepath.Join(dir, "[0-9][0-9]-*.html"))
	if err != nil {
		return err
	}
	for _, page := range pages {
		if !enabled[strings.TrimSuffix(filepath.Base(page), ".html")] {
			if err := os.Remove(page); err != nil {
				return fmt.Errorf("removing stale page: %w", err)
			}
		}
	}
	return nil
}

func writeStaticFile(outputDir, filename string, content []byte) error {
	outputPath := filepath.Join(outputDir, filename)
	if err := os.WriteFile(outputPath, content, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", filename, err)
	}
	fmt.Printf("✓ Generated %s\n", filename)
	return nil
}

// copyImages mirrors the images directory into the output. It is a no-op when
// the source directory does not exist, so the site builds fine without images.
func copyImages(srcDir, dstDir string) error {
	if _, err := os.Stat(srcDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if err := copyDir(srcDir, dstDir); err != nil {
		return err
	}

	fmt.Println("✓ Copied images/")
	return nil
}

func copyDir(srcDir, dstDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func markdownToHTML(markdown []byte, enabled map[string]bool) string {
	renderer := blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{
		Flags: blackfriday.CommonHTMLFlags,
	})
	html := blackfriday.Run(markdown,
		blackfriday.WithRenderer(renderer),
		blackfriday.WithExtensions(blackfriday.CommonExtensions),
	)
	return unlinkDisabled(fixRelativeLinks(string(html)), enabled)
}

func fixRelativeLinks(html string) string {
	re := regexp.MustCompile(`href="\.\./(README\.md|exercises/([^"]+)\.md)"`)
	html = re.ReplaceAllStringFunc(html, func(match string) string {
		if strings.Contains(match, "README.md") {
			return `href="index.html"`
		}
		re2 := regexp.MustCompile(`exercises/([^"]+)\.md`)
		matches := re2.FindStringSubmatch(match)
		if len(matches) > 1 {
			return fmt.Sprintf(`href="%s.html"`, matches[1])
		}
		return match
	})

	re = regexp.MustCompile(`href="(?:\./)?([0-9]{2}-[^"]+?)(?:\.es)?\.md(#[^"]*)?"`)
	html = re.ReplaceAllString(html, `href="$1.html$2"`)
	return html
}

var moduleLinkRe = regexp.MustCompile(`<a href="([0-9]{2}-[^"#]+)\.html(?:#[^"]*)?">(.*?)</a>`)

// unlinkDisabled turns links to modules that are not part of the build into
// plain text, so disabling a module never leaves dangling links behind.
func unlinkDisabled(html string, enabled map[string]bool) string {
	return moduleLinkRe.ReplaceAllStringFunc(html, func(match string) string {
		sub := moduleLinkRe.FindStringSubmatch(match)
		if enabled[sub[1]] {
			return match
		}
		return sub[2]
	})
}
