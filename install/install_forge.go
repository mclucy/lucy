package install

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/probe"
	tuiprogress "github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

func getForgeVersionFromPackageId(
p types.PackageId,
gameVersion types.RawVersion,
) (string, error) {
	if p.Version != types.VersionLatest && p.Version != types.VersionCompatible && p.Version != types.VersionAny && p.Version != types.VersionUnknown {
		return p.Version.String(), nil
	}
	return fetchForgeVersion(gameVersion)
}

func checkJavaAvailability() error {
	_, err := exec.LookPath("java")
	if err != nil {
		return errors.New("java not found in PATH, Forge requires Java to install")
	}
	return nil
}

var (
	forgeDocsURL       = "https://files.minecraftforge.net/"
	forgePromotionsURL = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
	forgeMavenBaseURL  = "https://maven.minecraftforge.net/net/minecraftforge/forge"

	// Forge/NeoForge installation differences (official docs):
	// 1) Artifact naming:
	//    Forge: forge-{mc_version}-{forge_version}-installer.jar
	//    NeoForge: neoforge-{version}-installer.jar
	// 2) Version metadata source:
	//    Forge: promotions_slim.json on files.minecraftforge.net
	//    NeoForge: release index from maven.neoforged.net
	// 3) Installation command:
	//    Both use: java -jar <installer>.jar --installServer
	forgeNeoForgeDiffDocURL = "https://docs.neoforged.net/user/docs/server"
)

type forgePromotions struct {
	Promos map[string]string `json:"promos"`
}

func init() {
	registerInstaller(types.PlatformForge, installForgeMod)
}

func installForgeMod(p types.Package) error {
	return installModLoaderPackage(p, types.PlatformForge)
}

func guardServerTopologyForForgePlatform() error {
	serverInfo := probe.ServerInfo()
	serverPlatform := serverInfo.Executable.DerivedModLoader()

	switch serverPlatform {
	case types.PlatformFabric, types.PlatformForge, types.PlatformNeoforge:
		return fmt.Errorf(
			"found an existing server platform %s, installation of forge aborted",
			serverPlatform.Title(),
		)
	}
	return nil
}

func promptSelectMinecraftVersionForForge() (version string) {
	manifest, err := fetchMojangVersionManifest()
	if err != nil || len(manifest.Versions) == 0 {
		return "error"
	}

	gameVersions := make([]string, 0, 20)
	for i := 0; i < len(manifest.Versions) && len(gameVersions) < 20; i++ {
		if manifest.Versions[i].Type == "release" {
			gameVersions = append(gameVersions, manifest.Versions[i].Id)
		}
	}

	var installLatest bool
	options := huh.NewOptions[string](gameVersions...)
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("No current Minecraft installation found.").
				Description("Do you want to install forge with its latest supported Minecraft version?").
				Affirmative("Yes, proceed").
				Negative("No, select a game version").
				Value(&installLatest),
		),
	).Run()
	if err != nil {
		return "none"
	}
	if installLatest {
		return gameVersions[0]
	}
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a Minecraft installation").
				Options(options...).
				Value(&version),
		).WithHide(installLatest),
	).Run()
	if err != nil {
		return "none"
	}
	return
}

func verifyForgeInstallation(workPath string) error {
	// Check for modern Forge (1.17+): libraries/ dir + launch script
	librariesPath := filepath.Join(workPath, "libraries")
	if _, err := os.Stat(librariesPath); err == nil {
		// libraries/ exists, check for launch scripts
		launchScripts := []string{
			"run.sh", "run.bat", "unix_args.txt", "win_args.txt",
		}
		for _, script := range launchScripts {
			if _, err := os.Stat(filepath.Join(workPath, script)); err == nil {
				return nil // Modern Forge verified
			}
		}
	}

	// Check for legacy Forge: forge-*-universal.jar or forge-*.jar
	entries, err := os.ReadDir(workPath)
	if err != nil {
		return fmt.Errorf(
			"verify forge installation failed: cannot read work directory: %w",
			err,
		)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.Contains(name, "forge-") && strings.HasSuffix(name, ".jar") {
			return nil // Legacy Forge verified
		}
	}

	return errors.New("forge installation verification failed: no artifacts found (expected libraries/ with launch scripts or forge-*.jar)")
}

func installForge(p types.PackageId) error {
	if err := guardServerTopologyForForgePlatform(); err != nil {
		return err
	}

	serverInfo := probe.ServerInfo()
	if serverInfo.WorkPath == "" {
		return errors.New("server working directory not found")
	}

	var gameVersion types.RawVersion
	switch serverInfo.Executable.DerivedModLoader() {
	case types.PlatformVanilla:
		gameVersion = serverInfo.Executable.GameVersion
	case types.PlatformNone:
		selectedVersion := promptSelectMinecraftVersionForForge()
		if selectedVersion == "none" || selectedVersion == "error" {
			return errors.New("minecraft version selection cancelled or failed")
		}
		gameVersion = types.RawVersion(selectedVersion)
	}

	if gameVersion == types.VersionUnknown {
		return fmt.Errorf(
			"unknown minecraft version, cannot infer forge bootstrap artifact; see %s",
			forgeDocsURL,
		)
	}

	if err := checkJavaAvailability(); err != nil {
		return err
	}

	if err := ensureMinecraftEULAAccepted(serverInfo.WorkPath); err != nil {
		return err
	}

	forgeVersion, err := getForgeVersionFromPackageId(p, gameVersion)
	if err != nil {
		return err
	}

	fileURL := resolveForgeInstallerURL(gameVersion, forgeVersion)

	tracker := tuiprogress.NewTracker("forge")
	defer tracker.Close()

	result, err := util.CachedDownload(
		fileURL,
		serverInfo.WorkPath,
		util.DownloadOptions{
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

	if result != nil {
		defer func() { _ = result.File.Close() }()
	}

	if result == nil {
		return errors.New("download result is nil")
	}

	installerTracker := tuiprogress.NewTrackerWithLogLimit(p.StringFull(), 5)
	defer installerTracker.Close()

	installerPath := result.File.Name()
	if err := runForgeInstaller(installerPath, installerTracker); err != nil {
		return err
	}

	installerTracker.SetPercent(0.99)
	if err := verifyForgeInstallation(serverInfo.WorkPath); err != nil {
		return err
	}

	probe.Rebuild()
	installerTracker.Complete("Forge installed")

	return nil
}

func fetchForgeVersion(gameVersion types.RawVersion) (string, error) {
	res, err := http.Get(forgePromotionsURL)
	if err != nil {
		return "", fmt.Errorf("fetch forge promotions failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf(
			"fetch forge promotions failed: status %d",
			res.StatusCode,
		)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("read forge promotions failed: %w", err)
	}

	var data forgePromotions
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("parse forge promotions failed: %w", err)
	}
	if len(data.Promos) == 0 {
		return "", fmt.Errorf("forge promotions is empty; see %s", forgeDocsURL)
	}

	keyBase := gameVersion.String()
	if v := data.Promos[keyBase+"-recommended"]; v != "" {
		return v, nil
	}
	if v := data.Promos[keyBase+"-latest"]; v != "" {
		return v, nil
	}

	return "", fmt.Errorf(
		"no forge version found for minecraft %s in promotions data; see %s (Forge) and %s (NeoForge comparison)",
		gameVersion,
		forgeDocsURL,
		forgeNeoForgeDiffDocURL,
	)
}

func resolveForgeInstallerURL(
gameVersion types.RawVersion,
forgeVersion string,
) string {
	combinedVersion := fmt.Sprintf("%s-%s", gameVersion.String(), forgeVersion)
	escaped := url.PathEscape(combinedVersion)
	return fmt.Sprintf(
		"%s/%s/forge-%s-installer.jar",
		forgeMavenBaseURL,
		escaped,
		escaped,
	)
}

// forgeStage represents a phase of the Forge installation process.
type forgeStage struct {
	name  string
	floor float64 // start of stage window [0, 1]
	span  float64 // width of stage window [0, 1]
}

// forgeStages defines the ordered installation phases with hardcoded progress windows.
// Based on observed Forge installer output patterns:
// 0.00-0.08: Initialization (JVM info, directory setup)
// 0.08-0.20: Extraction (main jar extraction)
// 0.20-0.60: Libraries (bulk of work - downloading/validating dependencies)
// 0.60-0.95: Processors (post-processing, server jar generation)
// 0.95-1.00: Verification (final checks, reprobe)
var forgeStages = []forgeStage{
	{name: "init", floor: 0.00, span: 0.02},
	{name: "libraries", floor: 0.02, span: 0.08},
	{name: "extract", floor: 0.10, span: 0.60},
	{name: "writing", floor: 0.70, span: 0.2},
	{name: "checksum", floor: 0.72, span: 0.03},
	{name: "processing", floor: 0.75, span: 0.22},
	{name: "completion", floor: 0.97, span: 0},
}

// forgeLogTail holds a bounded buffer of recent installer output lines.
type forgeLogTail struct {
	lines []string
	max   int
}

func newForgeLogTail(maxLines int) *forgeLogTail {
	return &forgeLogTail{lines: make([]string, 0, maxLines), max: maxLines}
}

func (t *forgeLogTail) append(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[1:]
	}
}

func (t *forgeLogTail) String() string {
	return strings.Join(t.lines, "\n")
}

// classifyForgeLine maps a log line to a stage index and returns whether it's a strong marker.
// Strong markers (true) advance the active stage; weak markers (false) only contribute to intra-stage progress.
func classifyForgeLine(line string) (stageIdx int, isStrong bool) {
	lower := strings.ToLower(line)

	// init stage
	if strings.Contains(lower, "jvm info") ||
	strings.Contains(lower, "current time") ||
	strings.Contains(lower, "target directory") {
		return 0, true
	}

	// libraries stage
	if strings.Contains(lower, "considering library") ||
	strings.Contains(lower, "downloading library") {
		return 1, false
	}
	if strings.Contains(lower, "downloading libraries") {
		return 1, true
	}

	// build & extract libraries stage
	if strings.Contains(lower, "building processors") {
		return 2, true
	}
	if strings.Contains(lower, "extracted") ||
	strings.Contains(lower, "output") {
		return 2, false
	}

	// writing stage
	if strings.Contains(lower, "writing output:") {
		return 3, true
	}

	// checksum stage
	if strings.Contains(lower, "loading patches file:") {
		return 4, true
	}
	if strings.Contains(lower, "reading patch") ||
	strings.Contains(lower, "checksum") {
		return 4, false
	}

	// processing stage
	if strings.Contains(lower, "processing:") {
		return 5, true
	}
	if strings.Contains(lower, "copying") ||
	strings.Contains(lower, "patching") {
		return 5, false
	}

	// completion stage marker
	if strings.Contains(lower, "The server installed successfully") {
		return 6, true
	}

	// Default: stay in current stage, weak marker
	return -1, false
}

// forgeAsymptoticProgress computes intra-stage progress using an asymptotic function.
// score: cumulative line count within the stage (0+)
// floor, span: stage window boundaries
// Returns a value in [floor, floor+span) that approaches floor+span asymptotically.
func forgeAsymptoticProgress(x float64, floor, span float64) float64 {
	const k = math.Ln10 * math.Ln2 * 4 // steepness of asymptotic curve
	// progress = floor + span * (1 - exp(-k * x))
	// As x → ∞, progress → floor + span
	progress := floor + span*math.Tanh(math.Log(x+1)/k)
	// Clamp to stage window to prevent overshoot
	if progress > floor+span {
		progress = floor + span
	}
	return progress
}

func runForgeInstaller(
installerPath string,
tracker *tuiprogress.Tracker,
) error {
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
	tail := newForgeLogTail(50)
	activeStageIdx := 0
	stageScores := make([]float64, len(forgeStages))
	var failurePhrase string

	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintln(logWriter, line)
		tail.append(line)

		// Detect explicit failure phrases
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

		stageIdx, isStrong := classifyForgeLine(line)
		if stageIdx >= 0 && stageIdx < len(forgeStages) &&
		isStrong && stageIdx > activeStageIdx {
			activeStageIdx = stageIdx
		}

		if activeStageIdx < len(forgeStages) {
			stageScores[activeStageIdx]++
			stage := forgeStages[activeStageIdx]
			progress := forgeAsymptoticProgress(
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
				"run forge installer failed: %s\nRecent output:\n%s",
				failurePhrase,
				tail.String(),
			)
		}
		return fmt.Errorf(
			"run forge installer failed: %w\nRecent output:\n%s",
			err,
			tail.String(),
		)
	}

	return nil
}
