package perf

// Logger is the subset of logging the reporter needs. A nil Logger discards messages.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

func infof(logger Logger, format string, args ...any) {
	if logger != nil {
		logger.Infof(format, args...)
	}
}

func warnf(logger Logger, format string, args ...any) {
	if logger != nil {
		logger.Warnf(format, args...)
	}
}

func errorf(logger Logger, format string, args ...any) {
	if logger != nil {
		logger.Errorf(format, args...)
	}
}
