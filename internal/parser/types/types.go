package types

import "context"

type Logger interface {
	Infof(format string, args ...any)
	Errorf(format string, args ...any)
}

type Request struct {
	WatcherName string
	FilePath    string
	Content     []byte
	Log         Logger
	OutputDir   string
}

type Result struct {
	Records int
}

type Function func(ctx context.Context, req Request) (Result, error)
