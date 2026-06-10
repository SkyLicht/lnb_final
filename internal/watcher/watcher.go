package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"lnb_tk/internal/config"
	"lnb_tk/internal/logger"
	"lnb_tk/internal/processor"
)

type Manager struct {
	log       *logger.Logger
	watchers  []config.WatcherConfig
	processor *processor.Processor
	scanEvery time.Duration

	mu      sync.Mutex
	running []*directoryWatcher
}

func NewManager(log *logger.Logger, watchers []config.WatcherConfig, processor *processor.Processor, scanEvery time.Duration) *Manager {
	if scanEvery < 0 {
		scanEvery = 0
	}
	return &Manager{
		log:       log,
		watchers:  watchers,
		processor: processor,
		scanEvery: scanEvery,
	}
}

func (m *Manager) Start(ctx context.Context) error {
	started := 0
	for _, cfg := range m.watchers {
		dw := &directoryWatcher{
			cfg:       cfg,
			log:       m.log,
			processor: m.processor,
			scanEvery: m.scanEvery,
		}
		if err := dw.start(ctx); err != nil {
			m.log.Errorf("watcher=%s dir=%s failed to start: %v", cfg.Name, cfg.FileDir, err)
			continue
		}

		m.mu.Lock()
		m.running = append(m.running, dw)
		m.mu.Unlock()
		started++
		m.log.Infof("watcher=%s type=%s dir=%s started", cfg.Name, cfg.WatcherType, cfg.FileDir)
	}

	if started == 0 {
		return fmt.Errorf("no watchers started")
	}
	return nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, w := range m.running {
		w.close()
	}
	m.running = nil
}

type directoryWatcher struct {
	cfg       config.WatcherConfig
	log       *logger.Logger
	processor *processor.Processor
	watcher   *fsnotify.Watcher
	scanEvery time.Duration
}

func (w *directoryWatcher) start(ctx context.Context) error {
	info, err := os.Stat(w.cfg.FileDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", w.cfg.FileDir)
	}

	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.watcher = fsWatcher

	if err := fsWatcher.Add(w.cfg.FileDir); err != nil {
		_ = fsWatcher.Close()
		return err
	}

	w.scanDirectory("startup")
	go w.loop(ctx)
	return nil
}

func (w *directoryWatcher) close() {
	if w.watcher != nil {
		_ = w.watcher.Close()
	}
}

func (w *directoryWatcher) loop(ctx context.Context) {
	var scanTicker *time.Ticker
	var scanC <-chan time.Time
	if w.scanEvery > 0 {
		scanTicker = time.NewTicker(w.scanEvery)
		defer scanTicker.Stop()
		scanC = scanTicker.C
	}

	for {
		select {
		case <-ctx.Done():
			w.close()
			return
		case <-scanC:
			w.scanDirectory("interval")
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.log.Errorf("watcher=%s fsnotify error: %v", w.cfg.Name, err)
		}
	}
}

func (w *directoryWatcher) handleEvent(event fsnotify.Event) {
	if !w.matches(event) {
		return
	}
	w.submitPath(event.Name, event.Op.String())
}

func (w *directoryWatcher) scanDirectory(source string) {
	entries, err := os.ReadDir(w.cfg.FileDir)
	if err != nil {
		w.log.Errorf("watcher=%s dir=%s scan=%s failed: %v", w.cfg.Name, w.cfg.FileDir, source, err)
		return
	}

	submitted := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if w.submitPath(filepath.Join(w.cfg.FileDir, entry.Name()), source) {
			submitted++
		}
	}
	if submitted > 0 {
		w.log.Infof("watcher=%s scan=%s queued=%d", w.cfg.Name, source, submitted)
	}
}

func (w *directoryWatcher) submitPath(path string, source string) bool {
	if isDirectory(path) {
		return false
	}
	if shouldIgnoreFile(path) {
		return false
	}
	errorDir := filepath.Join(w.cfg.FileDir, "error")
	accepted := w.processor.Submit(processor.Job{
		WatcherName:    w.cfg.Name,
		FilePath:       path,
		ParserFunction: w.cfg.Function,
		ErrorDir:       errorDir,
		OutputDir:      w.cfg.Output,
	})
	if accepted {
		w.log.Infof("watcher=%s source=%s file=%s queued", w.cfg.Name, source, path)
	}
	return accepted
}

func (w *directoryWatcher) matches(event fsnotify.Event) bool {
	switch w.cfg.WatcherType {
	case config.WatcherTypeFileCreated:
		return event.Has(fsnotify.Create)
	case config.WatcherTypeFileUpdated:
		return event.Has(fsnotify.Write)
	default:
		return false
	}
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func shouldIgnoreFile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(name, ".") {
		return true
	}
	for _, suffix := range []string{".tmp", ".temp", ".part", ".crdownload"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
