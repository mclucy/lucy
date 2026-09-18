package log

import "io"

// ShowRaw displays preformatted task output using the configured console sink.
// It adds no level prefix, newline, or history entry: child Lucy processes have
// already formatted their diagnostics, including any terminal escape sequences.
func ShowRaw(content string) error {
	_, err := io.WriteString(consoleOutput, content)
	return err
}

// RawOutput mirrors an external process stream to its display and log sinks.
// Unlike ReportInfo, it preserves bytes and stream boundaries without retaining
// an unbounded Minecraft console transcript in Lucy's diagnostic history.
func RawOutput(display, file io.Writer) io.Writer {
	return io.MultiWriter(display, file)
}
