package main

// headAssets is shared by every page: fonts, the site stylesheet, and the
// highlight.js / Font Awesome assets that app.js relies on.
const headAssets = `
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Lato:wght@400;700;900&family=Ubuntu+Mono:wght@400;700&display=swap">
  <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/atom-one-dark.min.css">
  <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.5.1/css/all.min.css">
  <script src="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/highlight.min.js"></script>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/languages/go.min.js"></script>`

const footerHTML = `
  <footer>
    <div class="container">
      <p>{{if eq .Lang "es"}}Workshop de Simpatía Mecánica en Go{{else}}Go Mechanical Sympathy Workshop{{end}}</p>
      <p>{{if eq .Lang "es"}}Creado por{{else}}Created by{{end}} <strong>Jesús Espino</strong></p>
      <div class="footer-links">
        <a href="https://github.com/jespino" target="_blank"><i class="fab fa-github"></i> GitHub</a>
        <a href="https://x.com/jespinog" target="_blank"><i class="fab fa-x-twitter"></i> @jespinog</a>
        <a href="https://linkedin.com/in/jesus-espino" target="_blank"><i class="fab fa-linkedin"></i> LinkedIn</a>
      </div>
    </div>
  </footer>`

const exerciseTemplate = `<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{if eq .Lang "es"}}Módulo{{else}}Module{{end}} {{printf "%02d" .Number}}: {{.Title}} — {{if eq .Lang "es"}}Simpatía Mecánica en Go{{else}}Go Mechanical Sympathy{{end}}</title>` + headAssets + `
  <link rel="stylesheet" href="{{.CSSPath}}">
</head>
<body data-chapter="{{.Slug}}">
  <nav class="navbar">
    <div class="container">
      <a href="{{.HomePath}}index.html" class="nav-home">{{if eq .Lang "es"}}Simpatía Mecánica en Go{{else}}Go Mechanical Sympathy{{end}}</a>
      <div class="nav-links">
        <a href="{{.HomePath}}index.html#modules">{{if eq .Lang "es"}}Todos los módulos{{else}}All modules{{end}}</a>
        <span class="progress-pill" data-progress-pill data-slugs="{{.AllSlugs}}" data-label="{{if eq .Lang "es"}}completados{{else}}complete{{end}}">
          <i class="fas fa-circle-check" aria-hidden="true"></i>
          <span data-progress-text>0/{{.TotalExercises}}</span>
        </span>
        <label class="lang-switch">
          <i class="fas fa-globe" aria-hidden="true"></i>
          <select onchange="if (this.value) location.href = this.value" aria-label="Language">
            <option value="" selected>{{.LangName}}</option>
            <option value="{{.AltLangURL}}">{{.AltLangName}}</option>
          </select>
        </label>
      </div>
    </div>
  </nav>

  <div class="container">
    <article class="module-content">
      <div class="module-meta">
        <span class="module-number">{{if eq .Lang "es"}}Módulo{{else}}Module{{end}} {{printf "%02d" .Number}}</span>
        <span class="module-part">{{.PartTitle}}</span>
      </div>
      {{.Content}}
    </article>

    <nav class="module-nav">
      {{if .PrevLink}}<a href="{{.PrevLink}}" class="nav-button">← {{.PrevTitle}}</a>{{else}}<span class="nav-spacer"></span>{{end}}
      <button class="complete-btn" data-complete
        data-label="{{if eq .Lang "es"}}Marcar como completado{{else}}Mark complete{{end}}"
        data-done-label="{{if eq .Lang "es"}}✓ Completado{{else}}✓ Completed{{end}}">
        {{if eq .Lang "es"}}Marcar como completado{{else}}Mark complete{{end}}
      </button>
      {{if .NextLink}}<a href="{{.NextLink}}" class="nav-button">{{.NextTitle}} →</a>{{else}}<span class="nav-spacer"></span>{{end}}
    </nav>
  </div>
` + footerHTML + `

  <script src="{{.AppJSPath}}"></script>
</body>
</html>
`

const indexTemplate = `<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.UI.HeroTitle}}</title>` + headAssets + `
  <link rel="stylesheet" href="{{.CSSPath}}">
</head>
<body>
  <nav class="navbar">
    <div class="container">
      <a href="{{.HomePath}}index.html" class="nav-home">{{.UI.HeroTitle}}</a>
      <div class="nav-links">
        <a href="#modules">{{if eq .Lang "es"}}Módulos{{else}}Modules{{end}}</a>
        <span class="progress-pill" data-progress-pill data-slugs="{{.AllSlugs}}" data-label="{{if eq .Lang "es"}}completados{{else}}complete{{end}}">
          <i class="fas fa-circle-check" aria-hidden="true"></i>
          <span data-progress-text>0/{{len .Exercises}}</span>
        </span>
        <label class="lang-switch">
          <i class="fas fa-globe" aria-hidden="true"></i>
          <select onchange="if (this.value) location.href = this.value" aria-label="Language">
            <option value="" selected>{{.LangName}}</option>
            <option value="{{.AltLangURL}}">{{.AltLangName}}</option>
          </select>
        </label>
      </div>
    </div>
  </nav>

  <div class="container">
    <header class="hero">
      <h1>{{.UI.HeroTitle}}</h1>
      <p class="lead">{{.UI.HeroLead}}</p>
      <ul class="hero-meta">
        <li>{{if eq .Lang "es"}}Duración{{else}}Duration{{end}} <strong>~2 {{if eq .Lang "es"}}días (modular){{else}}days (modular){{end}}</strong></li>
        <li>{{if eq .Lang "es"}}Formato{{else}}Format{{end}} <strong>{{if eq .Lang "es"}}Teoría + labs{{else}}Theory + labs{{end}}</strong></li>
        <li>{{if eq .Lang "es"}}Módulos{{else}}Modules{{end}} <strong>{{len .Exercises}}</strong></li>
        <li>Go <strong>1.27+</strong></li>
      </ul>
      <p class="hero-note">{{safeHTML .UI.HeroVersionNote}}</p>
    </header>

    <section class="prerequisites">
      <h2>{{.UI.Prerequisites}}</h2>
      <ul>
        {{range .UI.PrereqItems}}<li>{{safeHTML .}}</li>
        {{end}}
      </ul>
    </section>

    <section class="overview" id="modules">
      <h2>{{.UI.Overview}}</h2>
      <p>{{safeHTML .UI.OverviewText}}</p>
      {{range .Parts}}
      <h3 class="part-title">{{.Title}}</h3>
      <div class="modules-grid">
        {{range .Exercises}}
        <a href="{{.Filename}}" class="module-card-link">
          <div class="module-card" data-chapter="{{.Slug}}">
            <div class="module-card-head">
              <div class="module-number">{{$.UI.Exercise}} {{printf "%02d" .Number}}</div>
              <span class="module-status" aria-hidden="true"></span>
            </div>
            <h3>{{.Title}}</h3>
            <p>{{.Description}}</p>
          </div>
        </a>
        {{end}}
      </div>
      {{end}}
    </section>

    <section class="getting-started">
      <h2>{{.UI.GettingStarted}}</h2>
      <ol>
        {{range .UI.GettingStartedItems}}<li>{{safeHTML .}}</li>
        {{end}}
      </ol>
    </section>

    <section class="author">
      <h2>{{if eq .Lang "es"}}Autor del workshop{{else}}Workshop author{{end}}</h2>
      <div class="byline">
        <div class="byline-avatar">JE</div>
        <div>
          <strong>Jesús Espino</strong>
          <p>{{if eq .Lang "es"}}Desarrollador de Go con experiencia en sistemas de alto rendimiento y contribuciones a proyectos open source. Apasionado por enseñar cómo funciona Go por dentro y cómo sacarle partido.{{else}}Go practitioner with experience in high-performance systems and open source contributions. Passionate about teaching how Go works under the hood, and how to make the most of it.{{end}}</p>
        </div>
      </div>
    </section>

    <div class="cta">
      <a href="{{(index .Exercises 0).Filename}}" class="cta-button">{{.UI.CTAButton}}</a>
    </div>
  </div>
` + footerHTML + `

  <script src="{{.AppJSPath}}"></script>
</body>
</html>
`
