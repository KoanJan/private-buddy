package logger

// Debug logs at DEBUG level.
// Info logs at INFO level.
// Warn logs at WARN level.
// Error logs at ERROR level.
var Debug, Info, Warn, Error func(string, ...any)
