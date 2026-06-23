package logger

func Debug(msg string, args ...any) {
	globalInstance.Debug(msg, args...)
}

func Info(msg string, args ...any) {
	globalInstance.Info(msg, args...)
}

func Warn(msg string, args ...any) {
	globalInstance.Warn(msg, args...)
}

func Error(msg string, args ...any) {
	globalInstance.Error(msg, args...)
}
