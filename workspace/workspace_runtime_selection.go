package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"gopkg.in/yaml.v3"
)

func declaredRuntimeLock(root string) *lockfile.Document {
	declared, err := manifest.Read(root)
	if err != nil {
		return nil
	}
	locked, err := lockfile.Read(root)
	if err != nil {
		return nil
	}
	if declared.Server.Directory != locked.Server.Directory ||
		declared.Server.Minecraft != locked.Server.Minecraft ||
		declared.Server.Core.Product != locked.Server.Product ||
		(concreteRuntimeVersion(declared.Server.Core.ProductVersion) &&
			declared.Server.Core.ProductVersion != locked.Server.CoreVersion) {
		return nil
	}
	return locked
}

func (ws *Workspace) selectObservedRuntime(root string, locked *lockfile.Document) {
	if err := ws.Probe.observeOwnership(root); err != nil {
		ws.Warnings = append(ws.Warnings, err)
	}
	if locked != nil {
		ws.Probe.selected = true
		for _, candidate := range ws.Probe.Candidates {
			runtime := buildServerRuntime(candidate.Evidence)
			if runtime == nil || runtime.Product() != locked.Server.Product ||
				runtime.MinecraftVersion() != locked.Server.Minecraft ||
				runtime.Primary.Version != locked.Server.CoreVersion {
				continue
			}
			path := candidate.JarPath
			if locked.Server.EntryPoint != "" {
				path = filepath.Join(ws.ServerDir, filepath.FromSlash(locked.Server.EntryPoint))
			}
			selected, err := ws.Probe.Select(path)
			if err == nil {
				ws.Probe = selected
				return
			}
			// Installation can replace a pack launcher with generated output.
			// Only a receipt-verified entry point can replace the locked path.
			for _, point := range candidate.EntryPoints {
				if point.Managed {
					selected, err := ws.Probe.Select(point.Path)
					if err == nil {
						ws.Probe = selected
						return
					}
				}
			}
		}
		ws.Warnings = append(ws.Warnings, fmt.Errorf(
			"declared %s %s for Minecraft %s has no matching installed entry point; run lucy install --locked",
			locked.Server.Product, locked.Server.CoreVersion, locked.Server.Minecraft,
		))
	}
	ws.Probe.updateFindings()
}

func (p *Probe) observeOwnership(root string) error {
	scoped, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer scoped.Close()
	data, err := scoped.ReadFile(".lucy/install.yaml")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read installation ownership: %w", err)
	}
	var receipt struct {
		Version int `yaml:"version"`
		Entries []struct {
			Path   string `yaml:"path"`
			SHA256 string `yaml:"sha256"`
		} `yaml:"entries"`
	}
	if err := yaml.Unmarshal(data, &receipt); err != nil {
		return fmt.Errorf("read installation ownership: %w", err)
	}
	if receipt.Version != 1 {
		return fmt.Errorf("unsupported installation receipt version %d", receipt.Version)
	}
	owned := make(map[string]string, len(receipt.Entries))
	for _, entry := range receipt.Entries {
		if filepath.IsLocal(entry.Path) {
			owned[filepath.Clean(filepath.FromSlash(entry.Path))] = entry.SHA256
		}
	}
	for i := range p.Candidates {
		candidate := &p.Candidates[i]
		for j := range candidate.EntryPoints {
			point := &candidate.EntryPoints[j]
			relative, err := filepath.Rel(root, point.Path)
			if err != nil || !filepath.IsLocal(relative) || owned[relative] == "" {
				continue
			}
			point.Managed = receiptHashMatches(scoped, relative, owned[relative])
			if point.Managed && !candidate.EntryPoints[0].Managed {
				candidate.EntryPoints[0], candidate.EntryPoints[j] = *point, candidate.EntryPoints[0]
			}
		}
		if len(candidate.EntryPoints) > 0 {
			candidate.JarPath = candidate.EntryPoints[0].Path
			evidence := *candidate.Evidence
			evidence.PrimaryPath = candidate.JarPath
			candidate.Evidence = &evidence
		}
	}
	return nil
}

func receiptHashMatches(root *os.Root, path, expected string) bool {
	info, err := root.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	file, err := root.Open(path)
	if err != nil {
		return false
	}
	hash := sha256.New()
	_, readErr := io.Copy(hash, file)
	closeErr := file.Close()
	return readErr == nil && closeErr == nil && hex.EncodeToString(hash.Sum(nil)) == expected
}
