package modloader

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

func CheckJavaAvailability() error {
	_, err := exec.LookPath("java")
	if err != nil {
		return errors.New("java not found in PATH, mod loader installer requires Java")
	}
	return nil
}

type stage struct {
	name  string
	floor float64
	span  float64
}

var stages = []stage{
	{name: "init", floor: 0.00, span: 0.02},
	{name: "libraries", floor: 0.02, span: 0.08},
	{name: "extract", floor: 0.10, span: 0.60},
	{name: "writing", floor: 0.70, span: 0.2},
	{name: "checksum", floor: 0.72, span: 0.03},
	{name: "processing", floor: 0.75, span: 0.22},
	{name: "completion", floor: 0.97, span: 0},
}

type logTail struct {
	lines []string
	max   int
}

func newLogTail(maxLines int) *logTail {
	return &logTail{lines: make([]string, 0, maxLines), max: maxLines}
}

func (t *logTail) append(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[1:]
	}
}

func (t *logTail) String() string {
	return strings.Join(t.lines, "\n")
}

func classifyLine(line string) (stageIdx int, isStrong bool) {
	lower := strings.ToLower(line)

	if strings.Contains(lower, "jvm info") ||
		strings.Contains(lower, "current time") ||
		strings.Contains(lower, "target directory") {
		return 0, true
	}

	if strings.Contains(lower, "considering library") ||
		strings.Contains(lower, "downloading library") {
		return 1, false
	}
	if strings.Contains(lower, "downloading libraries") {
		return 1, true
	}

	if strings.Contains(lower, "building processors") {
		return 2, true
	}
	if strings.Contains(lower, "extracted") ||
		strings.Contains(lower, "output") {
		return 2, false
	}

	if strings.Contains(lower, "writing output:") {
		return 3, true
	}

	if strings.Contains(lower, "loading patches file:") {
		return 4, true
	}
	if strings.Contains(lower, "reading patch") ||
		strings.Contains(lower, "checksum") {
		return 4, false
	}

	if strings.Contains(lower, "processing:") {
		return 5, true
	}
	if strings.Contains(lower, "copying") ||
		strings.Contains(lower, "patching") {
		return 5, false
	}

	if strings.Contains(lower, "The server installed successfully") {
		return 6, true
	}

	return -1, false
}

func asymptoticProgress(x float64, floor, span float64) float64 {
	const k = math.Ln10 * math.Ln2 * 4
	progress := floor + span*math.Tanh(math.Log(x+1)/k)
	if progress > floor+span {
		return floor + span
	}
	return progress
}

func runInstallerJar(installerPath string, tracker *progress.Tracker) error {
	installerName := path.Base(installerPath)
	cmd := exec.Command("java", "-jar", installerName, "--installServer")
	cmd.Dir = path.Dir(installerPath)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe failed: %w", err)
	}

	merged := io.MultiReader(stdout, stderr)
	scanner := bufio.NewScanner(merged)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start installer failed: %w", err)
	}

	logWriter := tracker.LogWriter()
	tail := newLogTail(50)
	activeStageIdx := 0
	stageScores := make([]float64, len(stages))
	var failurePhrase string

	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintln(logWriter, line)
		tail.append(line)

		lower := strings.ToLower(line)
		if failurePhrase == "" {
			if strings.Contains(
				lower,
				"there was an error during installation",
			) {
				failurePhrase = "There was an error during installation"
			} else if strings.Contains(lower, "processor failed") {
				failurePhrase = "Processor failed"
			} else if strings.Contains(lower, "missing jar for processor") {
				failurePhrase = "Missing Jar for processor"
			}
		}

		stageIdx, isStrong := classifyLine(line)
		if stageIdx >= 0 && stageIdx < len(stages) &&
			isStrong && stageIdx > activeStageIdx {
			activeStageIdx = stageIdx
		}

		if activeStageIdx < len(stages) {
			stageScores[activeStageIdx]++
			stage := stages[activeStageIdx]
			progress := asymptoticProgress(
				stageScores[activeStageIdx],
				stage.floor,
				stage.span,
			)
			tracker.SetPercent(progress)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf(
			"read installer output failed: %w\nRecent output:\n%s",
			err,
			tail.String(),
		)
	}

	if err := cmd.Wait(); err != nil {
		if failurePhrase != "" {
			return fmt.Errorf(
				"run mod loader installer failed: %s\nRecent output:\n%s",
				failurePhrase,
				tail.String(),
			)
		}
		return fmt.Errorf(
			"run mod loader installer failed: %w\nRecent output:\n%s",
			err,
			tail.String(),
		)
	}

	return nil
}

func RunInstaller(
	id types.VersionedPackageRef,
	fileURL string,
	workPath string,
	platformName string,
) error {
	tracker := progress.NewTrackerWithLogging(id.StringFull(), 5)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = progress.WaitForShutdown(ctx)
	}()
	defer tracker.Close()

	result, err := cache.CachedDownload(
		fileURL,
		workPath,
		cache.DownloadOptions{
			Kind:               cache.KindArtifact,
			WrapReader:         tracker.ProxyReader,
			OnCacheHit:         tracker.CacheHit,
			OnResolvedFilename: func(title string) { tracker.SetTitle(title) },
			FileMode:           0o750,
		},
	)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	if result == nil {
		return errors.New("download result is nil")
	}
	defer func() { _ = result.File.Close() }()

	if err := runInstallerJar(result.File.Name(), tracker); err != nil {
		return err
	}

	tracker.SetPercent(0.99)
	workspace.Rebuild()
	tracker.Complete(platformName + " installed")
	return nil
}
