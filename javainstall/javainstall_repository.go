package javainstall

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"

	"github.com/mclucy/lucy/log"
)

// probeTimeout bounds a read-only package manager query. A query that exceeds
// it counts as a missing package.
const probeTimeout = 30 * time.Second

// repositoryKeyMaxBytes is the largest key file Lucy accepts. A vendor key is
// a few kilobytes.
const repositoryKeyMaxBytes = 1 << 20

// systemToolAvailable reports whether every named binary is on PATH. System
// package managers run only on Linux.
func systemToolAvailable(names ...string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			return false
		}
	}
	return true
}

// systemProbe runs a read-only query with a bounded wait. The error carries
// stderr so the caller can log why the query failed.
func systemProbe(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("empty system probe")
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(probeCtx, args[0], args[1:]...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return stdout.Bytes(), fmt.Errorf(
				"%s: %w",
				strings.Join(args, " "),
				err,
			)
		}
		return stdout.Bytes(), fmt.Errorf(
			"%s: %w: %s",
			strings.Join(args, " "),
			err,
			detail,
		)
	}
	return stdout.Bytes(), nil
}

var systemReleaseOnce = sync.OnceValue(readSystemRelease)

// systemRelease returns the fields of /etc/os-release. An absent file yields
// an empty map.
func systemRelease() map[string]string {
	return systemReleaseOnce()
}

func readSystemRelease() map[string]string {
	fields := map[string]string{}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		log.Debug("read os-release failed", "reason", err)
		return fields
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		fields[key] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return fields
}

// systemVersionMajor returns the leading release number, because vendor
// repositories name a distribution by its major version.
func systemVersionMajor(version string) string {
	major, _, _ := strings.Cut(version, ".")
	return major
}

// systemRPMArch maps the Go architecture to the RPM base architecture.
func systemRPMArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "386":
		return "i686"
	case "arm64":
		return "aarch64"
	case "ppc64le":
		return "ppc64le"
	case "s390x":
		return "s390x"
	default:
		return ""
	}
}

// addRepository writes the verified key and the source files, then runs the
// repository steps. Every write happens as root.
func addRepository(
	ctx context.Context,
	repo *Repository,
	host Host,
	out io.Writer,
) error {
	if repo == nil {
		return errors.New("no repository to add")
	}
	if repo.Key != nil {
		if err := addRepositoryKey(ctx, repo.Key, host, out); err != nil {
			return err
		}
	}
	for _, file := range repo.Files {
		if err := writeRootFile(
			ctx,
			file.Path,
			[]byte(file.Content),
			host,
			out,
		); err != nil {
			return fmt.Errorf("write %s: %w", file.Path, err)
		}
	}
	for _, step := range repo.Steps {
		if err := runStep(ctx, step, host, out); err != nil {
			return err
		}
	}
	return nil
}

func addRepositoryKey(
	ctx context.Context,
	key *RepositoryKey,
	host Host,
	out io.Writer,
) error {
	entity, err := fetchRepositoryKey(ctx, key)
	if err != nil {
		return err
	}
	data, err := serializeRepositoryKey(entity, key.Armored)
	if err != nil {
		return err
	}
	return writeRootFile(ctx, key.Path, data, host, out)
}

// fetchRepositoryKey downloads and verifies the pinned key. A key URL must
// use HTTPS, because the pin protects only the key that arrives.
func fetchRepositoryKey(
	ctx context.Context,
	key *RepositoryKey,
) (*openpgp.Entity, error) {
	address, err := url.Parse(key.URL)
	if err != nil {
		return nil, fmt.Errorf("parse repository key URL: %w", err)
	}
	if !strings.EqualFold(address.Scheme, "https") || address.Host == "" {
		return nil, fmt.Errorf(
			"repository key URL must use HTTPS: %s",
			key.URL,
		)
	}
	log.Debug("fetching repository key", "url", key.URL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, key.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("create repository key request: %w", err)
	}
	client := &http.Client{Timeout: probeTimeout}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch repository key: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"fetch repository key: status %s",
			response.Status,
		)
	}
	data, err := io.ReadAll(
		io.LimitReader(response.Body, repositoryKeyMaxBytes+1),
	)
	if err != nil {
		return nil, fmt.Errorf("read repository key: %w", err)
	}
	if int64(len(data)) > repositoryKeyMaxBytes {
		return nil, errors.New("repository key exceeds 1 MiB")
	}
	entity, err := verifyRepositoryKey(data, key.Fingerprint)
	if err != nil {
		return nil, err
	}
	return entity, nil
}

// verifyRepositoryKey returns the key whose primary fingerprint matches pin.
// The comparison ignores case and whitespace.
func verifyRepositoryKey(data []byte, pin string) (*openpgp.Entity, error) {
	entities, armoredErr := openpgp.ReadArmoredKeyRing(bytes.NewReader(data))
	if armoredErr != nil {
		binaryEntities, binaryErr := openpgp.ReadKeyRing(bytes.NewReader(data))
		if binaryErr != nil {
			return nil, fmt.Errorf(
				"parse OpenPGP key: %w",
				errors.Join(armoredErr, binaryErr),
			)
		}
		entities = binaryEntities
	}
	want := normalizeFingerprint(pin)
	if want == "" {
		return nil, errors.New("repository key has no pinned fingerprint")
	}
	for _, entity := range entities {
		got := strings.ToUpper(
			hex.EncodeToString(entity.PrimaryKey.Fingerprint[:]),
		)
		if got == want {
			return entity, nil
		}
	}
	return nil, fmt.Errorf(
		"repository key fingerprint mismatch: want %s",
		pin,
	)
}

func normalizeFingerprint(fingerprint string) string {
	return strings.ToUpper(strings.Join(strings.Fields(fingerprint), ""))
}

func serializeRepositoryKey(
	entity *openpgp.Entity,
	armored bool,
) ([]byte, error) {
	var buffer bytes.Buffer
	if !armored {
		if err := entity.Serialize(&buffer); err != nil {
			return nil, fmt.Errorf("serialize repository key: %w", err)
		}
		return buffer.Bytes(), nil
	}
	writer, err := armor.Encode(&buffer, openpgp.PublicKeyType, nil)
	if err != nil {
		return nil, fmt.Errorf("armor repository key: %w", err)
	}
	if err := entity.Serialize(writer); err != nil {
		return nil, fmt.Errorf("serialize repository key: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close repository key armor: %w", err)
	}
	return buffer.Bytes(), nil
}
