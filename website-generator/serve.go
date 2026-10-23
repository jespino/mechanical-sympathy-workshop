package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type devServer struct {
	opts siteOptions
	port int

	mu       sync.Mutex
	clients  map[chan struct{}]struct{}
	debounce *time.Timer
}

func newDevServer(opts siteOptions, port int) *devServer {
	return &devServer{
		opts:    opts,
		port:    port,
		clients: make(map[chan struct{}]struct{}),
	}
}

func (s *devServer) run() error {
	if err := s.rebuild(); err != nil {
		return fmt.Errorf("initial build failed: %w", err)
	}

	go s.watch()

	mux := http.NewServeMux()
	mux.HandleFunc("/--livereload", s.sseHandler)
	mux.Handle("/", s.injectLiveReload(http.FileServer(http.Dir(s.opts.outputDir))))

	addr := fmt.Sprintf(":%d", s.port)
	fmt.Printf("🌐 Dev server running at http://localhost:%d\n", s.port)
	fmt.Printf("👀 Watching %s and %s for changes...\n", s.opts.exercisesDir, s.opts.imagesDir)
	return http.ListenAndServe(addr, mux)
}

func (s *devServer) rebuild() error {
	fmt.Println("🔄 Rebuilding website...")
	if _, err := buildSite(s.opts); err != nil {
		return err
	}
	fmt.Println("✅ Rebuild complete")
	return nil
}

func (s *devServer) watch() {
	modTimes := make(map[string]time.Time)
	s.scanModTimes(modTimes)

	for {
		time.Sleep(500 * time.Millisecond)

		changed := false
		current := make(map[string]time.Time)
		s.scanModTimes(current)

		for path, modTime := range current {
			if prev, ok := modTimes[path]; !ok || !modTime.Equal(prev) {
				changed = true
				break
			}
		}
		if !changed {
			for path := range modTimes {
				if _, ok := current[path]; !ok {
					changed = true
					break
				}
			}
		}

		if changed {
			modTimes = current
			s.debouncedRebuild()
		}
	}
}

func (s *devServer) scanModTimes(out map[string]time.Time) {
	scanDir := func(dir, ext string) {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && filepath.Ext(path) == ext {
				out[path] = info.ModTime()
			}
			return nil
		})
	}

	scanDir(s.opts.exercisesDir, ".md")
	scanDir(s.opts.exercisesDir, ".json")

	// Watch every file in the images directory regardless of extension.
	filepath.Walk(s.opts.imagesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			out[path] = info.ModTime()
		}
		return nil
	})

	// Watch generator source files
	filepath.Walk(filepath.Dir(os.Args[0]), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && filepath.Ext(path) == ".go" {
			out[path] = info.ModTime()
		}
		return nil
	})
}

func (s *devServer) debouncedRebuild() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.debounce != nil {
		s.debounce.Stop()
	}
	s.debounce = time.AfterFunc(200*time.Millisecond, func() {
		if err := s.rebuild(); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Rebuild error: %v\n", err)
			return
		}
		s.notifyClients()
	})
}

func (s *devServer) notifyClients() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *devServer) sseHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	fmt.Fprintf(w, "data: connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-ch:
			fmt.Fprintf(w, "data: reload\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *devServer) injectLiveReload(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Ext(r.URL.Path) != ".html" && r.URL.Path != "/" && filepath.Ext(r.URL.Path) != "" {
			next.ServeHTTP(w, r)
			return
		}

		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		if filepath.Ext(path) == "" {
			path += ".html"
		}

		filePath := filepath.Join(s.opts.outputDir, filepath.Clean(path))
		content, err := os.ReadFile(filePath)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		script := `<script>
(function() {
    var es = new EventSource('/--livereload');
    es.onmessage = function(e) { if (e.data === 'reload') window.location.reload(); };
    es.onerror = function() { setTimeout(function() { window.location.reload(); }, 1000); };
})();
</script>`

		html := string(content)
		if idx := strings.LastIndex(html, "</body>"); idx >= 0 {
			html = html[:idx] + script + html[idx:]
		} else {
			html += script
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})
}
