package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type commandRunner struct {
	binary string
	home   string
}

func (r commandRunner) execute(ctx context.Context, root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := os.MkdirAll(r.home, 0o755); err != nil {
		return "", fmt.Errorf("create command home: %w", err)
	}
	overrides := map[string]string{
		"HOME":            r.home,
		"USERPROFILE":     r.home,
		"XDG_CACHE_HOME":  filepath.Join(r.home, "cache"),
		"XDG_CONFIG_HOME": filepath.Join(r.home, "config"),
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, overridden := overrides[strings.ToUpper(key)]; overridden {
			continue
		}
		environment = append(environment, entry)
	}
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	args = append(args, "--no-style")
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = root
	cmd.Env = environment
	cmd.WaitDelay = 5 * time.Second
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(output), fmt.Errorf("lucy %s: %w\n%s", strings.Join(args, " "), ctx.Err(), output)
	}
	if err != nil {
		return string(output), fmt.Errorf("lucy %s: %w\n%s", strings.Join(args, " "), err, output)
	}
	return string(output), nil
}
