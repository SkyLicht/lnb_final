package env

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Settings struct {
	ConfigPath      string
	Workers         int
	QueueSize       int
	StableFor       time.Duration
	RetryDelay      time.Duration
	MaxRetries      int
	ScanInterval    time.Duration
	MetricsInterval time.Duration
}

func LoadFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open env file %q: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("parse env file %q line %d: expected KEY=VALUE", path, lineNumber)
		}

		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" {
			return fmt.Errorf("parse env file %q line %d: empty key", path, lineNumber)
		}

		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("set env %q: %w", key, err)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read env file %q: %w", path, err)
	}
	return nil
}

func LoadSettings() (Settings, error) {
	workers, err := Int("WORKERS", 8)
	if err != nil {
		return Settings{}, err
	}

	queueSize, err := Int("QUEUE_SIZE", 2048)
	if err != nil {
		return Settings{}, err
	}

	stableFor, err := Duration("STABLE_FOR", 750*time.Millisecond)
	if err != nil {
		return Settings{}, err
	}

	retryDelay, err := Duration("RETRY_DELAY", 250*time.Millisecond)
	if err != nil {
		return Settings{}, err
	}

	maxRetries, err := Int("MAX_RETRIES", 40)
	if err != nil {
		return Settings{}, err
	}

	scanInterval, err := Duration("SCAN_INTERVAL", 30*time.Second)
	if err != nil {
		return Settings{}, err
	}

	metricsInterval, err := Duration("METRICS_INTERVAL", time.Minute)
	if err != nil {
		return Settings{}, err
	}

	return Settings{
		ConfigPath:      String("CONFIG_PATH", "config.json"),
		Workers:         workers,
		QueueSize:       queueSize,
		StableFor:       stableFor,
		RetryDelay:      retryDelay,
		MaxRetries:      maxRetries,
		ScanInterval:    scanInterval,
		MetricsInterval: metricsInterval,
	}, nil
}

func String(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func Int(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func Duration(key string, fallback time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration such as 750ms or 2s: %w", key, err)
	}
	return parsed, nil
}
