package crawl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// NDJSONWriter appends one JSON object per line to a file.
type NDJSONWriter struct {
	mu sync.Mutex
	f  *os.File
}

// OpenNDJSON creates (or truncates) an NDJSON output file.
func OpenNDJSON(path string) (*NDJSONWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, fmt.Errorf("crawl: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("crawl: open output %q: %w", path, err)
	}
	return &NDJSONWriter{f: f}, nil
}

// WriteLine marshals v as one JSON line.
func (w *NDJSONWriter) WriteLine(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.f.Write(b)
	return err
}

// Close flushes and closes the file.
func (w *NDJSONWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
