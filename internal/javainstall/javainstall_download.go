package javainstall

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mclucy/lucy/java"
)

const (
	foojayPackagesURL = "https://api.foojay.io/disco/v3.0/packages"
	foojayIDURL       = "https://api.foojay.io/disco/v3.0/ids/"

	// The package metadata only changes with releases, so a short TTL keeps
	// the latest build current without a network read on every plan.
	foojayCacheTTL = 12 * time.Hour
)

type download struct {
	url          string
	filename     string
	checksum     string
	archive      string
	size         int64
	javaVersion  string
	distribution java.Distribution
	destination  string
}

type foojayPackages struct {
	Result []foojayPackage `json:"result"`
}

// foojayPackage is one package of the package list and of the detailed
// /ids response. The detailed response carries the download URI and checksum.
type foojayPackage struct {
	ID                string `json:"id"`
	ArchiveType       string `json:"archive_type"`
	Distribution      string `json:"distribution"`
	Filename          string `json:"filename"`
	JavaVersion       string `json:"java_version"`
	Checksum          string `json:"checksum"`
	ChecksumType      string `json:"checksum_type"`
	ChecksumURI       string `json:"checksum_uri"`
	DirectDownloadURI string `json:"direct_download_uri"`
	Size              int64  `json:"size"`
}

// anyDistributionOrder lists the distributions that a request for any
// distribution tries in turn. foojay has no Temurin build for some old majors
// and platforms, such as Java 8 on Apple silicon.
var anyDistributionOrder = []java.Distribution{
	java.DistributionTemurin,
	java.DistributionZulu,
	java.DistributionCorretto,
	java.DistributionLiberica,
}

func (h *Host) downloadPlan(ctx context.Context, req Request) (*Plan, error) {
	architecture, ok := downloadArchitecture(h.goarch)
	if !ok {
		return nil, nil
	}
	operatingSystem, ok := downloadOperatingSystem(h.goos)
	if !ok {
		return nil, nil
	}
	distributions := []java.Distribution{req.Distribution}
	if req.Distribution == java.DistributionAny {
		distributions = anyDistributionOrder
	}
	for _, distribution := range distributions {
		plan, err := h.downloadPlanFor(ctx, req.Major, distribution, architecture, operatingSystem)
		if err != nil {
			return nil, err
		}
		if plan != nil {
			return plan, nil
		}
	}
	return nil, nil
}

// downloadPlanFor finds a verifiable download of one distribution. A runtime
// without the compiler is enough to run a server, so it asks for a JRE first.
func (h *Host) downloadPlanFor(
	ctx context.Context,
	major int,
	distribution java.Distribution,
	architecture string,
	operatingSystem string,
) (*Plan, error) {
	for _, packageType := range []string{"jre", "jdk"} {
		packages, err := h.fetchDownloadPackages(
			ctx,
			major,
			distribution,
			architecture,
			operatingSystem,
			packageType,
		)
		if err != nil {
			return nil, err
		}
		for _, pkg := range packages {
			if pkg.ID == "" {
				continue
			}
			build, err := h.fetchDownloadBuild(ctx, pkg.ID)
			if err != nil {
				h.logger.Debug(
					"java download package unavailable",
					"id", pkg.ID,
					"reason", err,
				)
				continue
			}
			if build.Checksum == "" && build.ChecksumURI != "" {
				build.Checksum, err = h.fetchChecksumFile(ctx, build.ChecksumURI)
				if err != nil {
					h.logger.Debug(
						"java download checksum unavailable",
						"id", pkg.ID,
						"reason", err,
					)
					continue
				}
			}
			download, ok := newDownload(pkg, build, distribution, major)
			if !ok {
				continue
			}
			root, err := h.managedRoot()
			if err != nil {
				return nil, fmt.Errorf("find the Java runtime directory: %w", err)
			}
			return &Plan{
				Method:       MethodDownload,
				Package:      download.filename,
				Distribution: distribution,
				Major:        major,
				root:         h.isRoot(),
				managedRoot:  root,
				download:     download,
			}, nil
		}
	}
	return nil, nil
}

// newDownload combines the package list entry with the detailed build entry.
// It reports false when the build cannot be verified or downloaded.
func newDownload(
	pkg foojayPackage,
	build foojayPackage,
	distribution java.Distribution,
	major int,
) (*download, bool) {
	checksumType := strings.ToLower(strings.TrimSpace(build.ChecksumType))
	if checksumType == "" {
		checksumType = strings.ToLower(strings.TrimSpace(pkg.ChecksumType))
	}
	if checksumType != "sha256" {
		return nil, false
	}
	checksum := strings.TrimSpace(build.Checksum)
	if checksum == "" {
		checksum = strings.TrimSpace(pkg.Checksum)
	}
	if checksum == "" {
		return nil, false
	}
	address := strings.TrimSpace(build.DirectDownloadURI)
	if address == "" {
		address = strings.TrimSpace(pkg.DirectDownloadURI)
	}
	if address == "" {
		return nil, false
	}
	filename := build.Filename
	if filename == "" {
		filename = pkg.Filename
	}
	if filename == "" {
		return nil, false
	}
	archive := pkg.ArchiveType
	if archive == "" {
		archive = downloadArchiveType(filename)
	}
	if archive != "tar.gz" && archive != "zip" {
		return nil, false
	}
	version := pkg.JavaVersion
	if version == "" {
		version = build.JavaVersion
	}
	if version == "" {
		version = strconv.Itoa(major)
	}
	return &download{
		url:          address,
		filename:     filename,
		checksum:     strings.ToLower(checksum),
		archive:      archive,
		size:         pkg.Size,
		javaVersion:  version,
		distribution: distribution,
		destination:  downloadDirectoryName(distribution, version),
	}, true
}

func (h *Host) fetchDownloadPackages(
	ctx context.Context,
	major int,
	distribution java.Distribution,
	architecture string,
	operatingSystem string,
	packageType string,
) ([]foojayPackage, error) {
	query := url.Values{}
	query.Set("version", strconv.Itoa(major))
	query.Set("distribution", string(distribution))
	query.Set("architecture", architecture)
	query.Set("operating_system", operatingSystem)
	musl := operatingSystem == "linux" && h.usesMusl()
	query.Set("lib_c_type", downloadLibC(musl, operatingSystem))
	query.Add("archive_type", "tar.gz")
	query.Add("archive_type", "zip")
	query.Set("package_type", packageType)
	query.Set("release_status", "ga")
	query.Set("latest", "available")
	query.Set("javafx_bundled", "false")
	query.Set("directly_downloadable", "true")
	return h.fetchFoojay(ctx, foojayPackagesURL+"?"+query.Encode())
}

func (h *Host) fetchDownloadBuild(ctx context.Context, id string) (foojayPackage, error) {
	builds, err := h.fetchFoojay(ctx, foojayIDURL+url.PathEscape(id))
	if err != nil {
		return foojayPackage{}, err
	}
	if len(builds) == 0 {
		return foojayPackage{}, errors.New("download package has no build")
	}
	return builds[0], nil
}

func (h *Host) fetchFoojay(
	ctx context.Context,
	address string,
) ([]foojayPackage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := h.fetchMetadata(ctx, address, foojayCacheTTL)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", address, err)
	}
	var packages foojayPackages
	if err := json.Unmarshal(data, &packages); err != nil {
		return nil, fmt.Errorf("parse %s: %w", address, err)
	}
	return packages.Result, nil
}

// fetchChecksumFile reads the sha256 digest from a checksum file that a
// vendor publishes next to its archive. The file is "<digest>" or
// "<digest>  <filename>". The digest then comes from the same origin as the
// archive, so it detects a damaged download but not a replaced release.
func (h *Host) fetchChecksumFile(ctx context.Context, address string) (string, error) {
	if !strings.HasPrefix(address, "https://") {
		return "", fmt.Errorf("checksum file %s is not served over HTTPS", address)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := h.fetchMetadata(ctx, address, foojayCacheTTL)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", address, err)
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "", fmt.Errorf("checksum file %s is empty", address)
	}
	digest, err := hex.DecodeString(fields[0])
	if err != nil || len(digest) != sha256.Size {
		return "", fmt.Errorf("checksum file %s has no sha256 digest", address)
	}
	return strings.ToLower(fields[0]), nil
}

func downloadArchitecture(goarch string) (string, bool) {
	switch goarch {
	case "amd64":
		return "x64", true
	case "arm64":
		return "aarch64", true
	default:
		return "", false
	}
}

func downloadOperatingSystem(goos string) (string, bool) {
	switch goos {
	case "linux":
		return "linux", true
	case "darwin":
		return "macos", true
	case "windows":
		return "windows", true
	default:
		return "", false
	}
}

// downloadLibC returns the libc that the download must match. Windows has no
// libc, but the API still expects a value.
func downloadLibC(musl bool, operatingSystem string) string {
	switch operatingSystem {
	case "macos":
		return "libc"
	case "windows":
		return "c_std_lib"
	default:
		if musl {
			return "musl"
		}
		return "glibc"
	}
}

func downloadArchiveType(filename string) string {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return "tar.gz"
	case strings.HasSuffix(lower, ".zip"):
		return "zip"
	default:
		return ""
	}
}

func downloadDirectoryName(
	distribution java.Distribution,
	version string,
) string {
	name := string(distribution) + "-" + version
	var builder strings.Builder
	for _, character := range name {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '.',
			character == '-',
			character == '_',
			character == '+':
			builder.WriteRune(character)
		default:
			builder.WriteRune('-')
		}
	}
	result := strings.Trim(builder.String(), ".-")
	if result == "" {
		return "java"
	}
	return result
}

func (d *download) describe(root string) []string {
	directory := d.destination
	if root != "" {
		directory = filepath.Join(root, d.destination)
	}
	return []string{
		"download " + d.url,
		"verify sha256 " + d.checksum,
		"extract into " + directory,
	}
}

// runDownload downloads, verifies, and extracts the archive under the
// per-user root captured by the plan. A directory that already exists is a
// finished installation.
func runDownload(
	ctx context.Context,
	host *Host,
	d *download,
	root string,
	out io.Writer,
) error {
	if d == nil || d.url == "" || d.checksum == "" {
		return errors.New("incomplete java download")
	}
	if root == "" {
		return errors.New("java runtime directory is unknown")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create java runtime directory: %w", err)
	}
	target := filepath.Join(root, d.destination)
	if installed, err := downloadTargetExists(target); err != nil {
		return err
	} else if installed {
		downloadProgress(out, "Already installed %s", target)
		return nil
	}

	archive, err := os.CreateTemp(root, ".lucy-java-download-*")
	if err != nil {
		return err
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)

	downloadProgress(
		out,
		"Downloading %s (%s)",
		d.filename,
		downloadSize(d.size),
	)
	if err := fetchDownloadArchive(ctx, host, d, archive); err != nil {
		archive.Close()
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}

	work, err := os.MkdirTemp(root, ".lucy-java-extract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	downloadProgress(out, "Extracting %s", d.filename)
	if err := extractDownloadArchive(archivePath, d.archive, work); err != nil {
		return fmt.Errorf("extract %s: %w", d.filename, err)
	}
	source := work
	if entry, ok := downloadSingleDirectory(work); ok {
		source = entry
	}

	if installed, err := downloadTargetExists(target); err != nil {
		return err
	} else if installed {
		downloadProgress(out, "Already installed %s", target)
		return nil
	}
	if err := os.Rename(source, target); err != nil {
		return fmt.Errorf("install java into %s: %w", target, err)
	}
	downloadProgress(out, "Installed %s", target)
	return nil
}

// downloadTargetExists reports whether path is an installed runtime. A file
// at path blocks the installation instead of counting as installed.
func downloadTargetExists(path string) (bool, error) {
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		return true, nil
	case err == nil:
		return false, fmt.Errorf("%s exists and is not a directory", path)
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

// fetchDownloadArchive streams the archive into dst and verifies the sha256
// checksum while the bytes arrive. A mismatch leaves dst incomplete.
// The request uses the invocation's configured HTTP client and context.
func fetchDownloadArchive(
	ctx context.Context,
	host *Host,
	d *download,
	dst io.Writer,
) error {
	response, err := host.request(ctx, d.url)
	if err != nil {
		return fmt.Errorf("download %s: %w", d.filename, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"download %s: server responded %s",
			d.filename,
			response.Status,
		)
	}
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dst, hash), response.Body); err != nil {
		return fmt.Errorf("download %s: %w", d.filename, err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, d.checksum) {
		return fmt.Errorf(
			"checksum of %s is %s, want %s",
			d.filename,
			actual,
			d.checksum,
		)
	}
	return nil
}

// extractDownloadArchive unpacks the archive into dir. Every file operation
// goes through a root, so an entry cannot reach outside dir, also not through
// a symbolic link that an earlier entry created.
func extractDownloadArchive(archivePath, archiveType, dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	switch archiveType {
	case "tar.gz":
		err = extractDownloadTarGz(archivePath, root)
	case "zip":
		err = extractDownloadZip(archivePath, root)
	default:
		err = fmt.Errorf("unsupported archive type %q", archiveType)
	}
	if err != nil {
		return err
	}
	return verifyDownloadLinks(root)
}

func extractDownloadTarGz(archivePath string, root *os.Root) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	compressed, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := extractDownloadTarEntry(reader, header, root); err != nil {
			return err
		}
	}
}

func extractDownloadTarEntry(reader io.Reader, header *tar.Header, root *os.Root) error {
	name, err := downloadEntryName(header.Name)
	if err != nil {
		return err
	}
	switch header.Typeflag {
	case tar.TypeDir:
		return root.MkdirAll(name, 0o755)
	case tar.TypeReg:
		return writeDownloadFile(root, name, reader, header.FileInfo().Mode())
	case tar.TypeSymlink:
		return writeDownloadSymlink(root, name, header.Linkname)
	case tar.TypeLink:
		target, err := downloadEntryName(header.Linkname)
		if err != nil {
			return err
		}
		if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		return root.Link(target, name)
	default:
		return fmt.Errorf(
			"archive entry %q has unsupported type %d",
			header.Name,
			header.Typeflag,
		)
	}
}

func extractDownloadZip(archivePath string, root *os.Root) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if err := extractDownloadZipEntry(entry, root); err != nil {
			return err
		}
	}
	return nil
}

func extractDownloadZipEntry(entry *zip.File, root *os.Root) error {
	name, err := downloadEntryName(entry.Name)
	if err != nil {
		return err
	}
	mode := entry.Mode()
	if mode&os.ModeSymlink != 0 {
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		target, readErr := io.ReadAll(io.LimitReader(reader, 4096))
		closeErr := reader.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		return writeDownloadSymlink(root, name, string(target))
	}
	if entry.FileInfo().IsDir() {
		return root.MkdirAll(name, 0o755)
	}
	if !mode.IsRegular() {
		return fmt.Errorf("archive entry %q has unsupported mode", entry.Name)
	}
	reader, err := entry.Open()
	if err != nil {
		return err
	}
	writeErr := writeDownloadFile(root, name, reader, mode)
	return errors.Join(writeErr, reader.Close())
}

func writeDownloadFile(root *os.Root, name string, reader io.Reader, mode os.FileMode) error {
	permissions := mode.Perm()
	if permissions == 0 {
		permissions = 0o644
	}
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	file, err := root.OpenFile(
		name,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		permissions,
	)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, reader); err != nil {
		return errors.Join(err, file.Close())
	}
	return file.Close()
}

// writeDownloadSymlink creates a link whose target stays inside the extraction
// directory when read from the link's own path. An absolute target would
// break after the rename. A link can still escape later, through a directory
// link that a later entry creates. verifyDownloadLinks catches that case.
func writeDownloadSymlink(root *os.Root, name, target string) error {
	local := filepath.FromSlash(target)
	if filepath.IsAbs(local) || filepath.VolumeName(local) != "" {
		return fmt.Errorf("archive symlink %q has an absolute target", name)
	}
	if !downloadContained(filepath.Join(filepath.Dir(name), local)) {
		return fmt.Errorf(
			"archive symlink %q points outside the extraction directory",
			name,
		)
	}
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return root.Symlink(local, name)
}

// verifyDownloadLinks rejects a symbolic link that does not resolve inside the
// root. A link target that does not exist is allowed. The check runs after
// extraction, because an entry can create the directory link that another
// link needs in order to escape.
func verifyDownloadLinks(root *os.Root) error {
	return fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		if _, err := root.Stat(filepath.FromSlash(name)); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf(
				"archive symlink %q does not resolve inside the extraction directory: %w",
				name,
				err,
			)
		}
		return nil
	})
}

// downloadEntryName turns an archive name into a path relative to the
// extraction root. It rejects absolute names and names that leave the root.
func downloadEntryName(name string) (string, error) {
	converted := filepath.FromSlash(name)
	if filepath.IsAbs(converted) || filepath.VolumeName(converted) != "" {
		return "", fmt.Errorf("archive entry %q is absolute", name)
	}
	cleaned := filepath.Clean(converted)
	if !downloadContained(cleaned) {
		return "", fmt.Errorf(
			"archive entry %q escapes the extraction directory",
			name,
		)
	}
	return cleaned, nil
}

// downloadContained reports whether a cleaned relative path stays inside its
// root.
func downloadContained(relative string) bool {
	return relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func downloadSingleDirectory(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		return "", false
	}
	if !entries[0].IsDir() {
		return "", false
	}
	return filepath.Join(dir, entries[0].Name()), true
}

func downloadProgress(out io.Writer, format string, args ...any) {
	if out == nil {
		return
	}
	_, _ = fmt.Fprintf(out, format+"\n", args...)
}

func downloadSize(size int64) string {
	const (
		kilobyte = 1 << 10
		megabyte = 1 << 20
	)
	switch {
	case size >= megabyte:
		return fmt.Sprintf("%.0f MB", float64(size)/megabyte)
	case size >= kilobyte:
		return fmt.Sprintf("%.0f KB", float64(size)/kilobyte)
	case size > 0:
		return fmt.Sprintf("%d bytes", size)
	default:
		return "unknown size"
	}
}
