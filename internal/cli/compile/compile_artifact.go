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

// outputDestination is where the compiled JAR is written: either an explicit
// JAR file or a directory that receives the project's artifact name.
type outputDestination struct {
	directory string
	file      string
}

// parseOutputDestination classifies the output flag. A path ending in `.jar`
// names the JAR to write; every other value, including an unset flag, names the
// directory that receives the project's artifact name.
func parseOutputDestination(value string) outputDestination {
	trimmed := strings.TrimSpace(value)
	if strings.EqualFold(filepath.Ext(trimmed), ".jar") {
		return outputDestination{file: trimmed}
	}
	if trimmed == "" {
		return outputDestination{directory: "."}
	}
	return outputDestination{directory: filepath.Clean(trimmed)}
}

// resolve returns the absolute destination for the artifact Gradle built as
// artifactName. A directory destination stores the JAR under that name, while
// an explicit destination keeps the file name the user asked for.
func (d outputDestination) resolve(artifactName string) (string, error) {
	target := d.file
	if d.directory != "" {
		target = filepath.Join(d.directory, artifactName)
	}
	resolved, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve output path: %w", err)
	}
	return resolved, nil
}

// ensureAbsent rejects an occupied destination; compile never overwrites files.
func ensureAbsent(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("output already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output: %w", err)
	}
	return nil
}

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
