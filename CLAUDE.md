# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## Repository Purpose

An educational workshop on **mechanical sympathy in Go**. It is a progressive (crescendo) workshop: each module explains one mechanism of the Go compiler, runtime or hardware, and has a lab where participants measure a before and an after with benchmarks (and pprof / compiler diagnostics / perf when useful). It uses stock Go 1.27, and participants do not build Go from source.

## Repository Architecture

```
exercises/          Module pages (NN-name.md + NN-name.es.md) + modules.json manifest
labs/               Single Go module (`module labs`), one package per lab: labs/NN-name/
images/             Image assets referenced from module pages (copied to website/images/)
diagrams/           Prompts (prompts.json) + generate.sh for the hand-drawn module diagrams
website-generator/  Go static site generator
website/            Generated output (gitignored)
Makefile
```

### Modules manifest

`exercises/modules.json` is the single source of truth for module order, titles and descriptions (en/es), the part each module belongs to, and whether it is `enabled`. The generator only renders enabled modules, unlinks links to disabled ones, and groups the index by part. `-only` / `-skip` (Makefile `ONLY=` / `SKIP=`) override it per build. Module numbers come from the slug prefix and stay stable.

### Lab conventions

Each `labs/NN-name/` package has:
- `before.go`: the naive baseline, never edited by participants
- `after.go` (`//go:build !solution`): the participant's copy, which starts identical to before with `After` suffixes and TODO hints
- `solution.go` (`//go:build solution`): the reference solution
- `lab_test.go`: a correctness test (after == before) plus benchmarks with `b.Loop()` and sub-benchmarks `impl=before` / `impl=after`, so `benchstat -col /impl` compares them

Some labs (02 profiling, 15 PGO) adapt this structure, and their pages document how.

### Module page structure

H1 title (no number) → intro → "Content You May Want to Check First" (when useful: bullets with a link, its kind and what it covers) → Learning Objectives → The Concept (real internals, verified against the Go source) → The Lab (steps: baseline with `make bench LAB=NN`, evidence, fix, compare) → real sample output (with the CPU named) → Understanding the Results → Key Takeaways → Going Further. Every module has an English (`.md`) and a Spanish (`.es.md`) version. Cross-module links use `NN-name.md`.

## Commands

```bash
make generate [ONLY=..] [SKIP=..]   # build the website from exercises/
make serve    [ONLY=..] [SKIP=..]   # dev server with live reload at localhost:8000
make clean                          # remove website/
make test-labs                      # vet + test every lab, with and without -tags solution
make bench LAB=NN [COUNT=6] [BENCH=regexp]   # benchmark after vs before, benchstat -col /impl
make solution LAB=NN                # same with the reference solution
make bench LAB=NN CPU=1,4,16        # pass -cpu to go test (contention labs)
make pgo                            # module 15: -pgo=off vs -pgo=<profile>
```

## Diagrams

Module diagrams are hand-drawn, xkcd-style images (no humor, no stick figures, white background, one teal accent) generated with codex image generation. Prompts live in `diagrams/prompts.json` (shared `style_prefix`/`style_suffix` + one entry per diagram with its anchor and EN/ES alt text). `diagrams/generate.sh <id>...` regenerates `images/<id>.webp`. Always look at the result: image models misspell labels and sometimes get the concept wrong. Pages reference them as `![alt](images/<id>.webp)` on their own line, at the same line in the EN and ES versions, and never between a sentence ending in ':' and the block it introduces.

## Adding a New Module

1. Create `exercises/NN-name.md` (English) and `exercises/NN-name.es.md` (Spanish)
2. Create `labs/NN-name/` following the lab conventions
3. Add an entry to `exercises/modules.json`
4. Run `make test-labs` and `make generate`

## Generator Code

- `main.go`: site build (`buildSite`), page generation, UI strings per language, link fixing/unlinking
- `manifest.go`: loading `modules.json` and selecting modules
- `templates.go`: HTML templates (module page, index page)
- `serve.go`: dev server with SSE live reload (watches `.md` and `.json` in exercises/)
- `static/`: CSS (same visual system as the observability workshop: Lato/Ubuntu Mono, purple→orange gradient, dark navbar/footer) and JS (highlight.js for tagged code blocks only, copy buttons, progress tracking in localStorage under `gms.*` keys)
