package compile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/types"
)

func selectArtifact(project gradleProject, platform types.Ecosystem) (string, error) {
	candidates := make([]archive, 0, len(project.Archives))
	seen := make(map[string]bool, len(project.Archives))
	for _, output := range project.Archives {
		if !output.Enabled || !strings.EqualFold(filepath.Ext(output.File), ".jar") || developmentClassifier(output.Classifier) {
			continue
		}
		if seen[output.File] {
			continue
		}
		info, err := os.Stat(output.File)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("inspect built archive %q: %w", output.File, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		packages, err := artifact.Analyze(output.File)
		if err != nil {
			return "", fmt.Errorf("verify built archive %q: %w", output.File, err)
		}
		matches := slices.ContainsFunc(packages, func(info artifact.Info) bool {
			return supportedPlatform(info.Ref.Eco) && (platform == "" || info.Ref.Eco == platform)
		})
		if matches {
			seen[output.File] = true
			candidates = append(candidates, output)
		}
	}
	published := make([]archive, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Published {
			published = append(published, candidate)
		}
	}
	if len(published) > 0 {
		candidates = published
	}
	if len(candidates) == 1 {
		return candidates[0].File, nil
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("project %s produced no deployable mod JAR with matching metadata", project.Path)
	}
	paths := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		paths = append(paths, candidate.File)
	}
	slices.Sort(paths)
	return "", fmt.Errorf("project %s produced multiple deployable mod JARs: %s", project.Path, strings.Join(paths, ", "))
}

func developmentClassifier(classifier string) bool {
	classifier = strings.ToLower(classifier)
	switch classifier {
	case "sources", "javadoc", "dev", "dev-shadow", "api", "test", "tests", "test-fixtures":
		return true
	default:
		return strings.HasSuffix(classifier, "-sources") || strings.HasSuffix(classifier, "-javadoc")
	}
}

func publishArtifact(ctx context.Context, source, destination string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open compiled artifact: %w", err)
	}
	defer func() { err = errors.Join(err, input.Close()) }()
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create output artifact: %w", err)
	}
	defer func() {
		if closeErr := output.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close output artifact: %w", closeErr))
		}
		if err != nil {
			if removeErr := os.Remove(destination); removeErr != nil {
				err = errors.Join(err, fmt.Errorf("remove partial output: %w", removeErr))
			}
		}
	}()
	if _, err := io.Copy(output, contextReader{ctx: ctx, reader: input}); err != nil {
		return fmt.Errorf("copy compiled artifact: %w", err)
	}
	return ctx.Err()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
