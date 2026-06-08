package logger

import (
	"io"
	"log"
	"os"
)

type Logger struct {
	info  *log.Logger
	error *log.Logger
}

func New(out io.Writer, errOut io.Writer) *Logger {
	return &Logger{
		info:  log.New(out, "INFO  ", log.Ldate|log.Ltime|log.Lmicroseconds),
		error: log.New(errOut, "ERROR ", log.Ldate|log.Ltime|log.Lmicroseconds),
	}
}

func Default() *Logger {
	return New(os.Stdout, os.Stderr)
}

func (l *Logger) Infof(format string, args ...any) {
	l.info.Printf(format, args...)
}

func (l *Logger) Errorf(format string, args ...any) {
	l.error.Printf(format, args...)
}
