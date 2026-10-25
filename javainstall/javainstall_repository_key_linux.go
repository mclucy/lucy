//go:build linux

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
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"

	"github.com/mclucy/lucy/log"
)

// repositoryKeyMaxBytes is the largest key file Lucy accepts. A vendor key is
// a few kilobytes.
const repositoryKeyMaxBytes = 1 << 20

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
