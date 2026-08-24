package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

const multiThreadThreshold = 10

// getExecutableInfo uses the new detector-based architecture to find server executables
func buildExecutableInfo() *ServerInstance {
	valid := make([]*detector.ExecutableEvidence, 0)
	workPath := workPath()
	// scanned counts the candidate executables that we analyzed.
	scanned := 0
	// contradicted is true when more than one specific detector matched one jar.
	contradicted := false

	// scanArtifact analyzes one candidate jar:
	// - no match: scanned increases
	// - one match: the match goes into valid
	// - more than one specific match: contradicted becomes true
	scanArtifact := func(jar string) {
		scanned++
		candidates := detector.Executable(jar)
		if candidates == nil || candidates.IsEmpty() {
			return
		}
		if candidates.IsAmbiguous() {
			contradicted = true
			return
		}
		valid = append(valid, candidates.Single())
	}
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
		log.Warn(fmt.Errorf("cannot read server directory: %w", err))
	}
	for _, jar := range jars {
		scanArtifact(jar)
	}

	// 2. Forge/Fabric installation paths
	// Will break after found
	fabricLib := filepath.Join(
		workPath, "libraries", "net", "fabricmc", "fabric-loader",
	)
	forgeLib := filepath.Join(
		workPath, "libraries", "net", "minecraftforge", "forge",
	)
	var forgeJars, fabricJars []string

	if stat, err := os.Stat(fabricLib); err == nil && stat.IsDir() {
		fabricJars, err = findJar(fabricLib)
		if err != nil {
			log.Warn(fmt.Errorf("cannot read fabric libraries: %w", err))
		}
	}

	if len(valid) == 0 {
		if stat, err := os.Stat(forgeLib); err == nil && stat.IsDir() {
			forgeJars, err = findJar(forgeLib)
			if err != nil {
				log.Warn(fmt.Errorf("cannot read forge libraries: %w", err))
			}
		}
	}
	jars = slices.Concat(forgeJars, fabricJars)

	for _, jar := range jars {
		scanArtifact(jar)
	}

	// 3. Everything under libraries
	if len(valid) == 0 {
		log.Info("no valid jar found yet, trying to find under libraries")
		jarPaths := findJarRecursive(filepath.Join(workPath, "libraries"))
		if len(jarPaths) >= multiThreadThreshold {
			mu := sync.Mutex{}
			wg := sync.WaitGroup{}
			for _, jarPath := range jarPaths {
				wg.Add(1)
				go func(jarPath string) {
					defer wg.Done()
					mu.Lock()
					scanArtifact(jarPath)
					mu.Unlock()
				}(jarPath)
			}
			wg.Wait()
		} else {
			for _, jarPath := range jarPaths {
				scanArtifact(jarPath)
			}
		}
	}

	// 4. pwd, recursively
	// TODO: Implement

	switch {
	case len(valid) == 1 && !contradicted:
		return buildServerInstance(valid[0])
	case len(valid) == 0 && scanned == 0:
		log.Info("no server executable found")
		return NoServer
	case len(valid) == 0:
		log.Info(fmt.Sprintf(
			"%d candidate executables examined, none identifiable as a server",
			scanned,
		))
		return UnknownServer
	default:
		if contradicted {
			log.Info("contradictory server environments detected")
		} else {
			log.Info("multiple parallel server environments detected")
		}
		return UnknownServer
	}
}

var getExecutableInfo = fn.Memoize(buildExecutableInfo)

func init() {
	resetProbeExecCache = func() {
		getExecutableInfo = fn.Memoize(buildExecutableInfo)
	}
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
		if fn.Exists(ext, filepath.Ext(entry.Name())) {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}

	return files, nil
}

const fileCountThreshold = 50000

func findJarRecursive(dir string) (jarFiles []string) {
	jarFiles = []string{}
	entries, _ := os.ReadDir(dir)
	var wg sync.WaitGroup
	var fileCount atomic.Int32
	var mu sync.Mutex

	sem := make(chan struct{}, 64)

	for _, entry := range entries {
		if fileCount.Load() >= fileCountThreshold {
			log.Info("file count threshold reached, stopping search")
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
			}(filepath.Join(dir, entry.Name()))
		} else {
			fileCount.Add(1)
			if filepath.Ext(entry.Name()) == ".jar" {
				mu.Lock()
				jarFiles = append(jarFiles, filepath.Join(dir, entry.Name()))
				mu.Unlock()
			}
		}
	}

	wg.Wait()
	return
}
