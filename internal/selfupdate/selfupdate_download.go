package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const (
	maxDownloadSize = 256 << 20
	maxBinarySize   = 256 << 20
)

func (s *Service) downloadAndReplace(
	ctx context.Context,
	opts Options,
	target *resolved,
	executable string,
) error {
	expected := strings.ToLower(strings.TrimSpace(target.file.SHA256))
	if expected == "" {
		if target.checksums == nil {
			return fmt.Errorf("release %s has no checksum for %s", target.manifest.Version, target.file.Name)
		}
		digest, err := s.fetchChecksum(ctx, target.checksums.String(), target.file.Name)
		if err != nil {
			return err
		}
		expected = digest
	}

	assetURL := target.base.JoinPath(path.Base(target.file.Name)).String()
	archive, err := os.CreateTemp("", "lucy-update-*")
	if err != nil {
		return fmt.Errorf("create the download file: %w", err)
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)

	if err := s.downloadFile(ctx, assetURL, expected, archive, target.file.Name, opts.Progress); err != nil {
		_ = archive.Close()
		return err
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("close the downloaded archive: %w", err)
	}

	staged, err := extractBinary(archivePath, s.goos, filepath.Dir(executable))
	if err != nil {
		return err
	}
	if err := s.verifyStaged(ctx, staged, target.manifest.Version); err != nil {
		_ = os.Remove(staged)
		return err
	}
	if err := replaceBinary(staged, executable); err != nil {
		_ = os.Remove(staged)
		return err
	}
	return nil
}

func (s *Service) downloadFile(
	ctx context.Context,
	rawURL string,
	expected string,
	dst *os.File,
	name string,
	progress func(string, int64, int64),
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("build the download request: %w", err)
	}
	req.Header.Set("user-agent", "lucy-self-update")
	s.logDebug("downloading release asset", "url", rawURL)

	resp, err := s.doer.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", name, resp.Status)
	}

	total := resp.ContentLength
	if total < 0 {
		total = 0
	}
	if total > maxDownloadSize {
		return fmt.Errorf("download %s exceeds %d bytes", name, maxDownloadSize)
	}

	var reader io.Reader = io.LimitReader(resp.Body, maxDownloadSize+1)
	if progress != nil {
		reader = &progressReader{reader: reader, name: name, total: total, progress: progress}
	}

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(dst, hasher), reader)
	if err != nil {
		return fmt.Errorf("download %s: %w", name, err)
	}
	if written > maxDownloadSize {
		return fmt.Errorf("download %s exceeds %d bytes", name, maxDownloadSize)
	}
	if expected != "" {
		actual := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actual, expected) {
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", name, expected, actual)
		}
	}
	if err := dst.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", name, err)
	}
	return nil
}

func (s *Service) fetchChecksum(ctx context.Context, rawURL, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("build the checksum request: %w", err)
	}
	req.Header.Set("user-agent", "lucy-self-update")

	resp, err := s.doer.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch checksums: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || len(fields[0]) != 64 {
			continue
		}
		if strings.TrimPrefix(fields[1], "*") == path.Base(name) {
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}
	return "", fmt.Errorf("checksum missing for %s", name)
}

func extractBinary(archivePath, goos, dir string) (string, error) {
	if goos == "windows" {
		return extractZipBinary(archivePath, dir)
	}
	return extractTarBinary(archivePath, dir)
}

func extractTarBinary(archivePath, dir string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open the release archive: %w", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return "", fmt.Errorf("read the release archive: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read the release archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Name != "lucy" && header.Name != "./lucy" {
			continue
		}
		return copyStaged(tarReader, dir, header.Size, "")
	}
	return "", errors.New("the release archive does not contain lucy")
}

func extractZipBinary(archivePath, dir string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("open the release archive: %w", err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		if file.Name != "lucy.exe" {
			continue
		}
		body, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("read the release archive: %w", err)
		}
		staged, copyErr := copyStaged(body, dir, int64(file.UncompressedSize64), ".exe")
		closeErr := body.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			_ = os.Remove(staged)
			return "", fmt.Errorf("read the release archive: %w", closeErr)
		}
		return staged, nil
	}
	return "", errors.New("the release archive does not contain lucy.exe")
}

func copyStaged(reader io.Reader, dir string, size int64, suffix string) (staged string, resultErr error) {
	if size < 0 || size > maxBinarySize {
		return "", fmt.Errorf("the new lucy is %d bytes", size)
	}
	file, err := os.CreateTemp(dir, ".lucy-update-*"+suffix)
	if err != nil {
		return "", fmt.Errorf("stage the new lucy: %w", err)
	}
	stagedPath := file.Name()
	staged = stagedPath
	defer func() {
		if file != nil {
			resultErr = errors.Join(resultErr, file.Close())
		}
		if resultErr != nil {
			_ = os.Remove(stagedPath)
		}
	}()

	written, err := io.Copy(file, io.LimitReader(reader, maxBinarySize+1))
	if err != nil {
		return "", fmt.Errorf("write the new lucy: %w", err)
	}
	if written == 0 {
		return "", errors.New("the release archive contains an empty lucy")
	}
	if written > maxBinarySize {
		return "", fmt.Errorf("the new lucy exceeds %d bytes", maxBinarySize)
	}
	if size > 0 && written != size {
		return "", fmt.Errorf("the new lucy is %d bytes, expected %d", written, size)
	}
	if err := file.Chmod(0o755); err != nil {
		return "", fmt.Errorf("make the new lucy executable: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync the new lucy: %w", err)
	}
	if err := file.Close(); err != nil {
		file = nil
		return "", fmt.Errorf("close the new lucy: %w", err)
	}
	file = nil
	return staged, nil
}

func (s *Service) verifyStaged(ctx context.Context, staged, version string) error {
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	command := exec.CommandContext(runCtx, staged, "--version")
	command.Env = append(os.Environ(), "LUCY_SELF_UPDATE_STAGED=1")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("the downloaded lucy did not start: %w", err)
	}
	if !hasVersionToken(string(output), version) {
		return fmt.Errorf(
			"the downloaded lucy reported %q instead of %s",
			strings.TrimSpace(string(output)),
			version,
		)
	}
	return nil
}

func hasVersionToken(output, version string) bool {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	for _, field := range strings.FieldsFunc(output, func(r rune) bool {
		return unicode.IsSpace(r) || r == '(' || r == ')'
	}) {
		if strings.TrimPrefix(field, "v") == version {
			return true
		}
	}
	return false
}

type progressReader struct {
	reader   io.Reader
	name     string
	total    int64
	read     int64
	progress func(string, int64, int64)
}

func (p *progressReader) Read(buffer []byte) (int, error) {
	n, err := p.reader.Read(buffer)
	p.read += int64(n)
	p.progress(p.name, p.read, p.total)
	return n, err
}
