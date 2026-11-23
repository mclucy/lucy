//go:build linux

package javainstall

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ProtonMail/gopenpgp/v3/armor"
	"github.com/ProtonMail/gopenpgp/v3/crypto"
)

// repositoryKeyMaxBytes is the largest key file Lucy accepts. A vendor key is
// a few kilobytes.
const repositoryKeyMaxBytes = 1 << 20

func addRepositoryKey(
	ctx context.Context,
	key *RepositoryKey,
	policy Policy,
	host *Host,
	input io.Reader,
	out io.Writer,
) error {
	entity, err := fetchRepositoryKey(ctx, key, host)
	if err != nil {
		return err
	}
	data, err := serializeRepositoryKey(entity, key.Armored)
	if err != nil {
		return err
	}
	return writeRootFile(ctx, key.Path, data, policy, host, input, out)
}

// fetchRepositoryKey downloads and verifies the pinned key. A key URL must
// use HTTPS, because the pin protects only the key that arrives.
func fetchRepositoryKey(
	ctx context.Context,
	key *RepositoryKey,
	host *Host,
) (*crypto.Key, error) {
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
	host.logger.Debug("fetching repository key", "url", key.URL)
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	response, err := host.request(probeCtx, key.URL)
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
func verifyRepositoryKey(data []byte, pin string) (*crypto.Key, error) {
	keys, err := repositoryKeys(data)
	if err != nil {
		return nil, err
	}
	want := normalizeFingerprint(pin)
	if want == "" {
		return nil, errors.New("repository key has no pinned fingerprint")
	}
	for _, key := range keys {
		if strings.ToUpper(key.GetFingerprint()) == want {
			return key, nil
		}
	}
	return nil, fmt.Errorf(
		"repository key fingerprint mismatch: want %s",
		pin,
	)
}

func repositoryKeys(data []byte) ([]*crypto.Key, error) {
	binary, armorErr := armor.UnarmorBytes(data)
	if armorErr == nil {
		ring, err := crypto.NewKeyRingFromBinary(binary)
		if err == nil {
			return ring.GetKeys(), nil
		}
	}
	ring, binaryErr := crypto.NewKeyRingFromBinary(data)
	if binaryErr != nil {
		return nil, fmt.Errorf(
			"parse OpenPGP key: %w",
			errors.Join(armorErr, binaryErr),
		)
	}
	return ring.GetKeys(), nil
}

func normalizeFingerprint(fingerprint string) string {
	return strings.ToUpper(strings.Join(strings.Fields(fingerprint), ""))
}

func serializeRepositoryKey(
	key *crypto.Key,
	armored bool,
) ([]byte, error) {
	data, err := key.Serialize()
	if err != nil {
		return nil, fmt.Errorf("serialize repository key: %w", err)
	}
	if !armored {
		return data, nil
	}
	text, err := armor.ArmorWithTypeBytes(data, "PGP PUBLIC KEY BLOCK")
	if err != nil {
		return nil, fmt.Errorf("armor repository key: %w", err)
	}
	return text, nil
}
