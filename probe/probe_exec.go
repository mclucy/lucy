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

	"charm.land/huh/v2"
)

const noteIgnorePath = "Some modding platforms are located from the libraries directory. " +
	"You might want to look at the platform and version, rather than the path."

const multiThreadThreshold = 10

// getExecutableInfo uses the new detector-based architecture to find server executables
func buildExecutableInfo() *types.RuntimeInfo {
	valid := make([]*detector.ExecutableEvidence, 0)
	workPath := workPath()
	for _, evidence := range detector.ForgeInstallationRuntimes(workPath) {
		valid = append(valid, evidence)
	}
	for _, evidence := range detector.NeoForgeInstallationRuntimes(workPath) {
		valid = append(valid, evidence)
	}

	// Layered search
	// 1. pwd
	// Proceed to step 2 no matter the result
	jars, err := findJar(workPath)
	if err != nil {
		logger.Warn(fmt.Errorf("cannot read server directory: %w", err))
	}
	for _, jar := range jars {
		candidates := detector.Executable(jar)
		if candidates == nil || candidates.IsEmpty() || candidates.IsAmbiguous() {
			continue
		}
		valid = append(valid, candidates.Single())
	}

	// 2. Forge/Fabric installation paths
	// Will break after found
	fabricLib := path.Join(
		workPath, "libraries", "net", "fabricmc", "fabric-loader",
	)
	forgeLib := path.Join(
		workPath, "libraries", "net", "minecraftforge", "forge",
	)
	var forgeJars, fabricJars []string

	if stat, err := os.Stat(fabricLib); err == nil && stat.IsDir() {
		fabricJars, err = findJar(fabricLib)
		if err != nil {
			logger.Warn(fmt.Errorf("cannot read fabric libraries: %w", err))
		}
	}

	if len(valid) == 0 {
		if stat, err := os.Stat(forgeLib); err == nil && stat.IsDir() {
			forgeJars, err = findJar(forgeLib)
			if err != nil {
				logger.Warn(fmt.Errorf("cannot read forge libraries: %w", err))
			}
		}
	}
	jars = slices.Concat(forgeJars, fabricJars)

	for _, jar := range jars {
		candidates := detector.Executable(jar)
		if candidates == nil || candidates.IsEmpty() || candidates.IsAmbiguous() {
			continue
		}
		valid = append(valid, candidates.Single())
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
					defer wg.Done()
					candidates := detector.Executable(jarPath)
					if candidates == nil || candidates.IsEmpty() || candidates.IsAmbiguous() {
						return
					}
					mu.Lock()
					valid = append(valid, candidates.Single())
					mu.Unlock()
				}(jarPath)
			}
			wg.Wait()
		} else {
			for _, jarPath := range jarPaths {
				candidates := detector.Executable(jarPath)
				if candidates == nil || candidates.IsEmpty() || candidates.IsAmbiguous() {
					continue
				}
				valid = append(valid, candidates.Single())
			}
		}
	}

	// 4. pwd, recursively
	// Prompt before do so due to the potential large number of files
	// TODO: Implement

	switch len(valid) {
	case 0:
		logger.Info("no server executable found")
		return types.NoExecutable
	case 1:
		return materializeRuntimeInfo(valid[0])
	default:
		runtimes := make([]*types.RuntimeInfo, 0, len(valid))
		for _, evidence := range valid {
			runtimes = append(runtimes, materializeRuntimeInfo(evidence))
		}
		choice := promptSelectExecutable(
			runtimes, []string{noteIgnorePath},
		)
		return materializeRuntimeInfo(valid[choice])
	}
}

var getExecutableInfo = tools.Memoize(buildExecutableInfo)

func init() {
	resetProbeExecCache = func() {
		getExecutableInfo = tools.Memoize(buildExecutableInfo)
	}
}

func promptSelectExecutable(
	executables []*types.RuntimeInfo,
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
				Filtering(true).
				Height(10).
				Value(&selection),
		),
	)
	if err := form.Run(); err != nil {
		logger.ShowWarn(err)
	}
	return selection
}

func generateNotes(notes ...string) string {
	var note strings.Builder
	for _, n := range notes {
		note.WriteString(tools.Cyan("*"))
		note.WriteString(" ")
		note.WriteString(n)
		note.WriteString("\n")
	}
	return note.String()
}

func executableLabel(executable *types.RuntimeInfo) string {
	return tools.Bold(executable.PrimaryEntrance) + " " + tools.Dim(executableAnnotation(executable))
}

func executableAnnotation(executable *types.RuntimeInfo) string {
	gameVersion := executable.GameVersion.String()
	derivedPlatform := executable.DerivedModLoader()
	if derivedPlatform == types.PlatformMinecraft {
		return fmt.Sprintf("(Minecraft %s, Vanilla)", gameVersion)
	}
	return fmt.Sprintf(
		"(Minecraft %s, %s %s)",
		gameVersion,
		derivedPlatform.Title(),
		executable.DerivedLoaderVersion(),
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

	sem := make(chan struct{}, 64)

	for _, entry := range entries {
		if atomic.LoadInt32(&fileCount) >= fileCountThreshold {
			logger.Info("file count threshold reached, stopping search")
			break
		}
		if entry.IsDir() {
			sem <- struct{}{}
			wg.Add(1)
			go func(subDir string) {
				defer func() { <-sem }()
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
