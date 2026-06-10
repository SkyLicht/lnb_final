package processor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"lnb_tk/internal/logger"
	"lnb_tk/internal/parser"
	"lnb_tk/internal/parser/types"
)

type Job struct {
	WatcherName    string
	FilePath       string
	ParserFunction string
	ErrorDir       string
	OutputDir      string
}

type Options struct {
	Workers         int
	QueueSize       int
	StableDuration  time.Duration
	RetryDelay      time.Duration
	MaxRetries      int
	MetricsInterval time.Duration
}

type Processor struct {
	log        *logger.Logger
	dispatcher *parser.Dispatcher
	opts       Options
	queue      chan Job
	done       chan struct{}
	wg         sync.WaitGroup

	mu       sync.Mutex
	inFlight map[string]struct{}
	stats    stats
}

type stats struct {
	submitted uint64
	duplicate uint64
	queueFull uint64
	processed uint64
	failed    uint64
}

func New(log *logger.Logger, dispatcher *parser.Dispatcher, opts Options) *Processor {
	opts = withDefaults(opts)
	return &Processor{
		log:        log,
		dispatcher: dispatcher,
		opts:       opts,
		queue:      make(chan Job, opts.QueueSize),
		done:       make(chan struct{}),
		inFlight:   make(map[string]struct{}),
	}
}

func (p *Processor) Start(ctx context.Context) {
	for i := 0; i < p.opts.Workers; i++ {
		workerID := i + 1
		p.wg.Add(1)
		go p.worker(ctx, workerID)
	}
	if p.opts.MetricsInterval > 0 {
		p.wg.Add(1)
		go p.metricsLoop(ctx)
	}
}

func (p *Processor) Stop() {
	close(p.done)
	p.wg.Wait()
}

func (p *Processor) Submit(job Job) bool {
	abs, err := filepath.Abs(job.FilePath)
	if err != nil {
		p.log.Errorf("watcher=%s file=%s resolve path failed: %v", job.WatcherName, job.FilePath, err)
		return false
	}
	job.FilePath = abs

	p.mu.Lock()
	if _, exists := p.inFlight[job.FilePath]; exists {
		p.mu.Unlock()
		atomic.AddUint64(&p.stats.duplicate, 1)
		return false
	}
	p.inFlight[job.FilePath] = struct{}{}
	p.mu.Unlock()

	select {
	case p.queue <- job:
		atomic.AddUint64(&p.stats.submitted, 1)
		return true
	default:
		p.release(job.FilePath)
		atomic.AddUint64(&p.stats.queueFull, 1)
		p.log.Errorf("watcher=%s file=%s queue is full; event dropped", job.WatcherName, job.FilePath)
		return false
	}
}

func (p *Processor) worker(ctx context.Context, workerID int) {
	defer p.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		case job := <-p.queue:
			p.handle(ctx, workerID, job)
			p.release(job.FilePath)
		}
	}
}

func (p *Processor) handle(ctx context.Context, workerID int, job Job) {
	err := p.safeProcess(ctx, job)
	if err != nil {
		atomic.AddUint64(&p.stats.failed, 1)
		p.log.Errorf("worker=%d watcher=%s file=%s failed: %v", workerID, job.WatcherName, job.FilePath, err)
		return
	}
	atomic.AddUint64(&p.stats.processed, 1)
	p.log.Infof("worker=%d watcher=%s file=%s processed and deleted", workerID, job.WatcherName, job.FilePath)
}

func (p *Processor) safeProcess(ctx context.Context, job Job) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic while processing file: %v", recovered)
		}
	}()
	return p.process(ctx, job)
}

func (p *Processor) metricsLoop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.opts.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		case <-ticker.C:
			p.log.Infof(
				"processor_stats submitted=%d processed=%d failed=%d duplicate=%d queue_full=%d in_flight=%d queue_depth=%d",
				atomic.LoadUint64(&p.stats.submitted),
				atomic.LoadUint64(&p.stats.processed),
				atomic.LoadUint64(&p.stats.failed),
				atomic.LoadUint64(&p.stats.duplicate),
				atomic.LoadUint64(&p.stats.queueFull),
				p.inFlightCount(),
				len(p.queue),
			)
		}
	}
}

func (p *Processor) process(ctx context.Context, job Job) error {
	if err := waitForStableFile(ctx, job.FilePath, p.opts.StableDuration, p.opts.RetryDelay, p.opts.MaxRetries); err != nil {
		if moved, moveErr := p.moveToErrorDir(job, err); moveErr != nil {
			return fmt.Errorf("%w; move to error dir failed: %v", err, moveErr)
		} else if moved {
			return fmt.Errorf("%w; moved to error dir", err)
		}
		return err
	}

	content, err := readWithRetry(ctx, job.FilePath, p.opts.RetryDelay, p.opts.MaxRetries)
	if err != nil {
		if moved, moveErr := p.moveToErrorDir(job, err); moveErr != nil {
			return fmt.Errorf("read failed: %w; move to error dir failed: %v", err, moveErr)
		} else if moved {
			return fmt.Errorf("read failed: %w; moved to error dir", err)
		}
		return fmt.Errorf("read failed: %w", err)
	}

	result, err := p.dispatcher.Parse(ctx, job.ParserFunction, types.Request{
		WatcherName: job.WatcherName,
		FilePath:    job.FilePath,
		Content:     content,
		Log:         p.log,
		OutputDir:   job.OutputDir,
	})
	if err != nil {
		if moved, moveErr := p.moveToErrorDir(job, err); moveErr != nil {
			return fmt.Errorf("parse failed: %w; move to error dir failed: %v", err, moveErr)
		} else if moved {
			return fmt.Errorf("parse failed: %w; moved to error dir", err)
		}
		return fmt.Errorf("parse failed: %w", err)
	}

	if err := os.Remove(job.FilePath); err != nil {
		return fmt.Errorf("delete processed file: %w", err)
	}

	p.log.Infof("watcher=%s file=%s parser=%s records=%d", job.WatcherName, job.FilePath, job.ParserFunction, result.Records)
	return nil
}

func (p *Processor) moveToErrorDir(job Job, cause error) (bool, error) {
	if job.ErrorDir == "" {
		return false, nil
	}
	if errors.Is(cause, os.ErrNotExist) {
		return false, nil
	}

	if err := os.MkdirAll(job.ErrorDir, 0755); err != nil {
		return false, err
	}

	name := filepath.Base(job.FilePath)
	target := filepath.Join(job.ErrorDir, fmt.Sprintf("%s.%d.error", name, time.Now().UTC().UnixNano()))
	if err := os.Rename(job.FilePath, target); err == nil {
		p.log.Errorf("watcher=%s file=%s moved to error file=%s cause=%v", job.WatcherName, job.FilePath, target, cause)
		return true, nil
	}

	if err := copyFile(job.FilePath, target); err != nil {
		return false, err
	}
	if err := os.Remove(job.FilePath); err != nil {
		return false, err
	}
	p.log.Errorf("watcher=%s file=%s copied to error file=%s cause=%v", job.WatcherName, job.FilePath, target, cause)
	return true, nil
}

func (p *Processor) release(filePath string) {
	p.mu.Lock()
	delete(p.inFlight, filePath)
	p.mu.Unlock()
}

func (p *Processor) inFlightCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.inFlight)
}

func waitForStableFile(ctx context.Context, path string, stableDuration time.Duration, retryDelay time.Duration, maxRetries int) error {
	var previous os.FileInfo
	stableSince := time.Time{}

	for attempt := 0; attempt < maxRetries; attempt++ {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("path is a directory")
		}

		if previous != nil && info.Size() == previous.Size() && info.ModTime().Equal(previous.ModTime()) {
			if stableSince.IsZero() {
				stableSince = time.Now()
			}
			if time.Since(stableSince) >= stableDuration {
				return nil
			}
		} else {
			stableSince = time.Time{}
			previous = info
		}

		if err := sleep(ctx, retryDelay); err != nil {
			return err
		}
	}

	return fmt.Errorf("file did not become stable after %d attempts", maxRetries)
}

func readWithRetry(ctx context.Context, path string, retryDelay time.Duration, maxRetries int) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if err := sleep(ctx, retryDelay); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func copyFile(source string, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func withDefaults(opts Options) Options {
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = 1024
	}
	if opts.StableDuration <= 0 {
		opts.StableDuration = 750 * time.Millisecond
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = 250 * time.Millisecond
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 40
	}
	if opts.MetricsInterval < 0 {
		opts.MetricsInterval = 0
	}
	return opts
}
