package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"

	"lnb_tk/internal/config"
	"lnb_tk/internal/logger"
	"lnb_tk/internal/processor"
)

type Manager struct {
	log       *logger.Logger
	watchers  []config.WatcherConfig
	processor *processor.Processor

	mu      sync.Mutex
	running []*directoryWatcher
}

func NewManager(log *logger.Logger, watchers []config.WatcherConfig, processor *processor.Processor) *Manager {
	return &Manager{
		log:       log,
		watchers:  watchers,
		processor: processor,
	}
}

func (m *Manager) Start(ctx context.Context) error {
	started := 0
	for _, cfg := range m.watchers {
		dw := &directoryWatcher{
			cfg:       cfg,
			log:       m.log,
			processor: m.processor,
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

	go w.loop(ctx)
	return nil
}

func (w *directoryWatcher) close() {
	if w.watcher != nil {
		_ = w.watcher.Close()
	}
}

func (w *directoryWatcher) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			w.close()
			return
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
	if isDirectory(event.Name) {
		return
	}

	errorDir := filepath.Join(w.cfg.FileDir, "error")
	accepted := w.processor.Submit(processor.Job{
		WatcherName:    w.cfg.Name,
		FilePath:       event.Name,
		ParserFunction: w.cfg.Function,
		ErrorDir:       errorDir,
	})
	if accepted {
		w.log.Infof("watcher=%s event=%s file=%s queued", w.cfg.Name, event.Op.String(), event.Name)
	}
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
