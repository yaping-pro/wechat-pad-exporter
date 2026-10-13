package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// RotatingLogWriter is an io.Writer that rotates log files based on max bytes and keep count.
type RotatingLogWriter struct {
	mu       sync.Mutex
	filePath string
	maxBytes int64
	keep     int
	file     *os.File
	currSize int64
}

// NewRotatingLogWriter creates or opens a rotating log file.
func NewRotatingLogWriter(filePath string, maxBytes int64, keep int) (*RotatingLogWriter, error) {
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024 // 10MB default
	}
	if keep < 1 {
		keep = 1
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log dir %s: %w", dir, err)
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", filePath, err)
	}

	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to stat log file %s: %w", filePath, err)
	}

	return &RotatingLogWriter{
		filePath: filePath,
		maxBytes: maxBytes,
		keep:     keep,
		file:     f,
		currSize: fi.Size(),
	}, nil
}

// Write writes bytes to the log file and rotates if maxBytes is exceeded.
func (w *RotatingLogWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	writeLen := int64(len(p))
	if w.currSize+writeLen > w.maxBytes {
		if rotErr := w.rotate(); rotErr != nil {
			// Write to current file even if rotation fails
			fmt.Fprintf(os.Stderr, "[RotatingLogWriter] rotation error: %v\n", rotErr)
		}
	}

	n, err = w.file.Write(p)
	w.currSize += int64(n)
	return n, err
}

// rotate performs the file rotation step. Must be called with w.mu locked.
func (w *RotatingLogWriter) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}

	// Rotate existing files: .2 -> .3, .1 -> .2, etc.
	for i := w.keep - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", w.filePath, i)
		dst := fmt.Sprintf("%s.%d", w.filePath, i+1)
		_ = os.Rename(src, dst)
	}

	// Rename main log file to .1
	_ = os.Rename(w.filePath, fmt.Sprintf("%s.1", w.filePath))

	// Re-open fresh main log file
	f, err := os.OpenFile(w.filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	w.file = f
	w.currSize = 0
	return nil
}

// Close closes the underlying log file.
func (w *RotatingLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// MultiWriterWithStderr creates an io.Writer that writes to both rotating log writer and os.Stderr.
func MultiWriterWithStderr(rw *RotatingLogWriter) io.Writer {
	if rw == nil {
		return os.Stderr
	}
	return io.MultiWriter(os.Stderr, rw)
}
