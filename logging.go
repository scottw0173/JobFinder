package main

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
)

type closeFunc func() error

func initLogger(logFile string) (*slog.Logger, closeFunc, error) {
	handlers := []slog.Handler{
		tint.NewHandler(os.Stderr, &tint.Options{
			Level:   slog.LevelDebug,
			NoColor: false}),
	}
	closers := []closeFunc{}
	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("log file %s: %w", logFile, err)
		}
		bufferedFile := bufio.NewWriterSize(file, 8192)
		closeFile := func() error {
			if err := bufferedFile.Flush(); err != nil {
				return fmt.Errorf("flush log file %s: %w", logFile, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("close log file %s: %w", logFile, err)
			}
			return nil
		}
		handlers = append(handlers, slog.NewJSONHandler(bufferedFile, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		}))
		closers = append(closers, closeFile)
	}
	close := func() error {
		var errs []error
		for _, close := range closers {
			if err := close(); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	return slog.New(slog.NewMultiHandler(handlers...)), close, nil
}
