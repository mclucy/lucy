package server

import (
	"fmt"
	"html"
	"strings"
)

// renderSystemdDaemon renders the shared daemon's systemd service definition.
func renderSystemdDaemon(binary string) string {
	return `[Unit]
Description=Lucy daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + systemdExecutable(binary) + ` run-daemon
Restart=on-failure
RestartSec=3s

[Install]
WantedBy=multi-user.target
`
}

// renderSystemdInstanceTemplate renders a server template that lets the runner stop gracefully.
func renderSystemdInstanceTemplate(binary string) string {
	return `[Unit]
Description=Lucy Minecraft server %i
After=network-online.target lucyd.service
Wants=network-online.target lucyd.service

[Service]
Type=simple
ExecStart=` + systemdExecutable(binary) + ` run-server %i
Restart=on-failure
RestartSec=10s
KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=90s

[Install]
WantedBy=multi-user.target
`
}

// systemdExecutable encodes one executable token, escaping unit specifiers and C quoting.
// The ':' executable prefix disables environment expansion in argv, including a '$' in the path.
func systemdExecutable(binary string) string {
	var b strings.Builder
	b.WriteString(`":`)
	for _, c := range []byte(binary) {
		switch {
		case c == '%':
			b.WriteString("%%")
		case c == '\\' || c == '"':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// renderLaunchdDaemon renders an always-running shared daemon with native log files.
func renderLaunchdDaemon(binary string) string {
	return plist(map[string]any{
		"Label":             LaunchdDaemonLabel(),
		"ProgramArguments":  []string{binary, "run-daemon"},
		"KeepAlive":         true,
		"RunAtLoad":         true,
		"StandardOutPath":   LogDir() + "/lucyd.out.log",
		"StandardErrorPath": LogDir() + "/lucyd.err.log",
	})
}

// renderLaunchdInstance renders a server job that starts when explicitly loaded.
func renderLaunchdInstance(inst Instance, binary string) string {
	return plist(map[string]any{
		"Label":             inst.LaunchdLabel,
		"ProgramArguments":  []string{binary, "run-server", inst.Name},
		"WorkingDirectory":  inst.Root,
		"UserName":          inst.RunUser,
		"RunAtLoad":         true,
		"KeepAlive":         map[string]any{"SuccessfulExit": false},
		"StandardOutPath":   inst.Root + "/logs/lucy-service.out.log",
		"StandardErrorPath": inst.Root + "/logs/lucy-service.err.log",
	})
}

// plist renders the supported launchd keys in a stable top-level order.
func plist(values map[string]any) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	for _, key := range []string{
		"Label",
		"ProgramArguments",
		"WorkingDirectory",
		"UserName",
		"KeepAlive",
		"RunAtLoad",
		"StandardOutPath",
		"StandardErrorPath",
	} {
		value, ok := values[key]
		if !ok {
			continue
		}
		writePlistKey(&b, key, value)
	}
	b.WriteString(`</dict>
</plist>
`)
	return b.String()
}

// writePlistKey appends an XML-escaped dictionary key and its value.
func writePlistKey(b *strings.Builder, key string, value any) {
	b.WriteString("\t<key>")
	b.WriteString(html.EscapeString(key))
	b.WriteString("</key>\n")
	writePlistValue(b, value, 1)
}

// writePlistValue appends a supported launchd scalar, string array or nested dictionary.
func writePlistValue(b *strings.Builder, value any, indent int) {
	prefix := strings.Repeat("\t", indent)
	switch v := value.(type) {
	case bool:
		b.WriteString(prefix)
		if v {
			b.WriteString("<true/>\n")
		} else {
			b.WriteString("<false/>\n")
		}
	case string:
		b.WriteString(prefix)
		b.WriteString("<string>")
		b.WriteString(html.EscapeString(v))
		b.WriteString("</string>\n")
	case []string:
		b.WriteString(prefix)
		b.WriteString("<array>\n")
		for _, item := range v {
			writePlistValue(b, item, indent+1)
		}
		b.WriteString(prefix)
		b.WriteString("</array>\n")
	case map[string]any:
		b.WriteString(prefix)
		b.WriteString("<dict>\n")
		for k, item := range v {
			writePlistKey(b, k, item)
		}
		b.WriteString(prefix)
		b.WriteString("</dict>\n")
	}
}
