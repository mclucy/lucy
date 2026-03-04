package probe

import (
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe/internal/detector"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"

	"github.com/charmbracelet/huh"
)

var (
	UnknownExecutable = detector.UnknownExecutable
	NoExecutable      = detector.NoExecutable
)

const noteSuspectPrePackagedServer = "This is likely a pre-packaged server. Therefore, you might want to ignore the paths, and only look for the executable with your expected game version and mod loader."

const multiThreadThreshold = 10

// getExecutableInfo uses the new detector-based architecture to find server executables
func buildExecutableInfo() *types.ExecutableInfo {
	var valid []*types.ExecutableInfo
	workPath := workPath()

	// Layered search
	// 1. pwd
	// Proceed to step 2 no matter the result
	jars, err := findJar(workPath)
	if err != nil {
		logger.Warn(fmt.Errorf("cannot read server directory: %w", err))
	}
	for _, jar := range jars {
		exec := detector.Executable(jar)
		if exec == nil {
			continue
		}
		valid = append(valid, exec)
	}

	// 2. Forge/Fabric installation paths
	// Will break after found
	fabricLib := path.Join(workPath, "libraries", "net", "fabricmc")
	forgeLib := path.Join(workPath, "libraries", "net", "minecraftforge")
	var forgeJars, fabricJars []string

	if stat, err := os.Stat(fabricLib); err == nil && stat.IsDir() {
		fabricJars, err = findJar(fabricLib)
		if err != nil {
			logger.Warn(fmt.Errorf("cannot read fabric libraries: %w", err))
		}
	}

	if stat, err := os.Stat(forgeLib); err == nil && stat.IsDir() {
		forgeJars, err = findJar(forgeLib)
		if err != nil {
			logger.Warn(fmt.Errorf("cannot read forge libraries: %w", err))
		}
	}
	jars = slices.Concat(forgeJars, fabricJars)

	for _, jar := range jars {
		exec := detector.Executable(jar)
		if exec == nil {
			continue
		}
		valid = append(valid, exec)
	}

	// 3. Everything under libraries
	if len(valid) == 0 {
		logger.Info("no valid jar found yet, trying to find under libraries")
		jarPaths := findJarRecursive(path.Join(workPath, "libraries"))
		if len(jarPaths) >= multiThreadThreshold {
			mu := sync.Mutex{}
			wg := sync.WaitGroup{}
			for _, jarPath := range jarPaths {
				wg.Add(1)
				go func(jarPath string) {
					exec := detector.Executable(jarPath)
					if exec == nil {
						wg.Done()
						return
					}
					mu.Lock()
					valid = append(valid, exec)
					mu.Unlock()
					wg.Done()
				}(jarPath)
			}
			wg.Wait()
		} else {
			for _, jarPath := range jarPaths {
				exec := detector.Executable(jarPath)
				if exec == nil {
					continue
				}
				valid = append(valid, exec)
			}
		}
	}

	// 4. pwd, recursively
	// Prompt before do so due to the potential large number of files
	// TODO: Implement
	switch len(valid) {
	case 0:
		logger.Info("no server executable found")
		return NoExecutable
	case 1:
		return valid[0]
	default:
		choice := selectExecutable(
			valid,
			[]string{noteSuspectPrePackagedServer},
		)
		return valid[choice]
	}
}

var getExecutableInfo = tools.Memoize(buildExecutableInfo)

func init() {
	resetProbeExecCache = func() {
		getExecutableInfo = tools.Memoize(buildExecutableInfo)
	}
}

func selectExecutable(
	executables []*types.ExecutableInfo,
	notes []string,
) int {
	selection := 0
	title := "Multiple possible executables detected, select one"
	noteText := strings.TrimSpace(generateNotes(notes...))
	if noteText != "" {
		title = title + "\n" + noteText
	}

	options := make([]huh.Option[int], 0, len(executables))
	for i, exec := range executables {
		options = append(options, huh.NewOption(executableLabel(exec), i))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title(title).
				Options(options...).
				Value(&selection),
		),
	)
	if err := form.Run(); err != nil {
		logger.ShowWarn(err)
	}
	return selection
}

func generateNotes(notes ...string) string {
	var note string
	for _, n := range notes {
		note += tools.Cyan("*") + " " + n + "\n"
	}
	return note
}

func executableLabel(executable *types.ExecutableInfo) string {
	return tools.Bold(executable.Path) + " " + tools.Dim(executableAnnotation(executable))
}

func executableAnnotation(executable *types.ExecutableInfo) string {
	gameVersion := executable.GameVersion.String()
	if executable.ModLoader == types.PlatformMinecraft {
		return fmt.Sprintf("(Minecraft %s, Vanilla)", gameVersion)
	}
	return fmt.Sprintf(
		"(Minecraft %s, %s %s)",
		gameVersion,
		executable.ModLoader.Title(),
		executable.LoaderVersion.String(),
	)
}

func findJar(dir ...string) (jarFiles []string, err error) {
	jarFiles = []string{}
	for _, d := range dir {
		files, err := findFileWithExt(d, ".jar")
		if err != nil {
			return nil, err
		}
		jarFiles = append(jarFiles, files...)
	}
	return jarFiles, nil
}

func findFileWithExt(dir string, ext ...string) (files []string, err error) {
	files = []string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if tools.Exists(ext, path.Ext(entry.Name())) {
			files = append(files, path.Join(dir, entry.Name()))
		}
	}

	return files, nil
}

const fileCountThreshold = 50000

func findJarRecursive(dir string) (jarFiles []string) {
	jarFiles = []string{}
	entries, _ := os.ReadDir(dir)
	var wg sync.WaitGroup
	var fileCount int32
	var mu sync.Mutex

	// TODO: Use semaphore to limit the number of goroutines
	for _, entry := range entries {
		if atomic.LoadInt32(&fileCount) >= fileCountThreshold {
			logger.Info("file count threshold reached, stopping search")
			break
		}
		if entry.IsDir() {
			wg.Add(1)
			go func(subDir string) {
				defer wg.Done()
				subJarFiles := findJarRecursive(subDir)
				mu.Lock()
				jarFiles = append(jarFiles, subJarFiles...)
				mu.Unlock()
			}(path.Join(dir, entry.Name()))
		} else {
			atomic.AddInt32(&fileCount, 1)
			if path.Ext(entry.Name()) == ".jar" {
				mu.Lock()
				jarFiles = append(jarFiles, path.Join(dir, entry.Name()))
				mu.Unlock()
			}
		}
	}

	wg.Wait()
	return
}
