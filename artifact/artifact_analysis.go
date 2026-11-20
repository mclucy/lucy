package artifact

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/mclucy/lucy/catalog"
)

// descriptorInputs memoizes immutable parsed root entries while leaving
// inventory and loader projections independent.
type descriptorInputs struct {
	entries sync.Map
}

func descriptorInput[T any](inputs *descriptorInputs, key string, parse func() (T, error)) (T, error) {
	if inputs == nil {
		return parse()
	}
	if saved, ok := inputs.entries.Load(key); ok {
		return saved.(func() (T, error))()
	}
	saved, _ := inputs.entries.LoadOrStore(key, sync.OnceValues(parse))
	return saved.(func() (T, error))()
}

// Analyses retains evidence for the files observed in one operation. It is
// not a replacement for verifying bytes at an ownership or store boundary.
// The zero value is ready for use.
type Analyses struct {
	files sync.Map // clean path -> *Analysis
}

func (a *Analyses) File(path string) *Analysis {
	path = filepath.Clean(path)
	if a != nil {
		if file, ok := a.files.Load(path); ok {
			return file.(*Analysis)
		}
	}
	file := &Analysis{path: path}
	file.initial = sync.OnceValues(func() (os.FileInfo, error) { return os.Stat(path) })
	file.info = sync.OnceValues(func() ([]PackageInfo, error) {
		return readObserved(file, func() ([]PackageInfo, error) { return readInfo(path, &file.inputs) })
	})
	file.hash = sync.OnceValues(func() (analysisHashes, error) { return readObserved(file, file.readHashes) })
	file.murmur = sync.OnceValues(func() (uint32, error) { return readObserved(file, File{Path: path}.MurmurHash) })
	if a != nil {
		actual, _ := a.files.LoadOrStore(path, file)
		return actual.(*Analysis)
	}
	return file
}

// Analysis holds local package evidence. Returned descriptor values are
// read-only. Native views depend on the loader and Minecraft version.
type Analysis struct {
	path    string
	initial func() (os.FileInfo, error)
	info    func() ([]PackageInfo, error)
	hash    func() (analysisHashes, error)
	murmur  func() (uint32, error)
	views   sync.Map // descriptorViewKey -> func() (DescriptorView, error)
	inputs  descriptorInputs
}

type descriptorViewKey struct {
	loader    catalog.Ecosystem
	minecraft string
}

// DescriptorView contains the server-visible modules and the side evidence
// from the same native descriptor traversal.
type DescriptorView struct {
	Modules    []catalog.DescriptorModule
	ClientOnly bool
	Known      bool
}

func (a *Analysis) Info() ([]PackageInfo, error) { return a.info() }

func (a *Analysis) Descriptor(ctx context.Context, loader catalog.Ecosystem, minecraft string) (DescriptorView, error) {
	if err := ctx.Err(); err != nil {
		return DescriptorView{}, err
	}
	key := descriptorViewKey{loader: loader, minecraft: minecraft}
	if view, ok := a.views.Load(key); ok {
		return view.(func() (DescriptorView, error))()
	}
	read := sync.OnceValues(func() (DescriptorView, error) {
		modules, err := readObserved(a, func() ([]catalog.DescriptorModule, error) {
			return readModulesFor(ctx, a.path, loader, minecraft, &a.inputs)
		})
		if err != nil {
			return DescriptorView{}, err
		}
		clientOnly, known := descriptorSideResult(modules)
		return DescriptorView{Modules: normalizeModules(modules), ClientOnly: clientOnly, Known: known}, nil
	})
	view, _ := a.views.LoadOrStore(key, read)
	return view.(func() (DescriptorView, error))()
}

// Observe one filesystem version across the independent reads that contribute
// evidence. Publication also checks the digest, because metadata is not a
// content proof.
func readObserved[T any](a *Analysis, read func() (T, error)) (T, error) {
	var zero T
	if err := a.checkFile(); err != nil {
		return zero, err
	}
	result, err := read()
	if err != nil {
		return zero, err
	}
	if err := a.checkFile(); err != nil {
		return zero, err
	}
	return result, nil
}

func (a *Analysis) checkFile() error {
	baseline, err := a.initial()
	if err != nil {
		return err
	}
	current, err := os.Stat(a.path)
	if err != nil {
		return err
	}
	if !os.SameFile(baseline, current) || !current.Mode().IsRegular() || baseline.Mode() != current.Mode() ||
		baseline.Size() != current.Size() || !baseline.ModTime().Equal(current.ModTime()) {
		return fmt.Errorf("artifact changed during analysis: %s", a.path)
	}
	return nil
}

type analysisHashes struct {
	sha1   [sha1.Size]byte
	sha256 [sha256.Size]byte
}

func (a *Analysis) SHA1() ([sha1.Size]byte, error) {
	hashes, err := a.hash()
	return hashes.sha1, err
}

func (a *Analysis) SHA256() (string, error) {
	hashes, err := a.hash()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hashes.sha256[:]), nil
}

func (a *Analysis) MurmurHash() (uint32, error) { return a.murmur() }

func (a *Analysis) readHashes() (analysisHashes, error) {
	file, err := os.Open(a.path)
	if err != nil {
		return analysisHashes{}, fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	one, two := sha1.New(), sha256.New()
	if _, err := io.Copy(io.MultiWriter(one, two), file); err != nil {
		return analysisHashes{}, fmt.Errorf("hash artifact: %w", err)
	}
	var result analysisHashes
	one.Sum(result.sha1[:0])
	two.Sum(result.sha256[:0])
	return result, nil
}
