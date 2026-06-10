package slugresolve

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"strconv"

	"github.com/mclucy/lucy/slugmap"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
)

type localArtifact struct {
	data []byte
	sha1 [sha1.Size]byte
}

func newLocalArtifact(filePath string) (localArtifact, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return localArtifact{}, err
	}
	return localArtifact{data: data, sha1: sha1.Sum(data)}, nil
}

func (a localArtifact) Sha1() [sha1.Size]byte {
	return a.sha1
}

func (a localArtifact) CurseForgeFingerprint() uint32 {
	return curseForgeFingerprint(a.data)
}

func (a localArtifact) hashForSource(src types.SourceId) string {
	switch src {
	case types.SourceCurseForge:
		return strconv.FormatUint(uint64(a.CurseForgeFingerprint()), 10)
	default:
		return hex.EncodeToString(a.sha1[:])
	}
}

func ResolveSlug(
	src types.SourceId,
	localId string,
	filePath string,
	metadataURLs []string,
) string {
	var fileHash string
	var artifact localArtifact
	if filePath != "" {
		var err error
		artifact, err = newLocalArtifact(filePath)
		if err == nil {
			fileHash = artifact.hashForSource(src)
		}
	}

	if fileHash != "" {
		if slug, ok := slugmap.Default().Get(src, localId, fileHash); ok {
			return slug
		}
	}

	if filePath != "" && fileHash != "" {
		provider, ok, err := routing.GetArtifactMapper(src)
		if err == nil && ok {
			remote, resolvedHash, err := provider.Mapper.NameByHash(artifact)
			if err == nil && remote.RemoteName != "" {
				if resolvedHash != "" {
					fileHash = resolvedHash
				}
				slugmap.Default().Set(src, localId, fileHash, remote.RemoteName, "hash")
				return remote.RemoteName
			}
		}
	}

	return localId
}

func curseForgeFingerprint(data []byte) uint32 {
	const multiplex uint32 = 1540483477

	normalizedLen := uint32(0)
	for _, b := range data {
		if !isCFWhitespace(b) {
			normalizedLen++
		}
	}

	h := uint32(1) ^ normalizedLen
	var pending uint32
	var pendingBits uint32

	for _, b := range data {
		if isCFWhitespace(b) {
			continue
		}
		pending |= uint32(b) << pendingBits
		pendingBits += 8
		if pendingBits == 32 {
			k := pending * multiplex
			k = (k ^ k>>24) * multiplex
			h = h*multiplex ^ k
			pending = 0
			pendingBits = 0
		}
	}

	if pendingBits > 0 {
		h = (h ^ pending) * multiplex
	}

	h = (h ^ h>>13) * multiplex
	return h ^ h>>15
}

func isCFWhitespace(b byte) bool {
	return b == 0x09 || b == 0x0A || b == 0x0D || b == 0x20
}
