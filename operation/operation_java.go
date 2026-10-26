package operation

import (
	"context"
	"errors"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/java"
	"github.com/mclucy/lucy/log"
)

// JavaChange edits declared Java requirements. A nil field stays unchanged.
type JavaChange struct {
	// Version is the major version that lucy.yaml pins; zero returns it to
	// auto.
	Version *int
	// Distribution is the distribution that lucy.yaml pins; the unspecified
	// distribution returns it to auto.
	Distribution *java.Distribution
}

// JavaChangeResult reports the declared Java requirements after a change.
type JavaChangeResult struct {
	// Java is the override in lucy.yaml; nil means auto.
	Java              *document.ManifestJava `json:"java,omitempty"`
	PublishedManifest bool                   `json:"published_manifest"`
}

// SetJava changes only the manifest. Installation reconciles the lock.
func SetJava(ctx context.Context, root string, change JavaChange) (result *JavaChangeResult, resultErr error) {
	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return nil, err
	}
	d, err := document.ManifestFile(root).Read()
	if err != nil {
		return nil, err
	}
	result = &JavaChangeResult{}
	if change.Version != nil || change.Distribution != nil {
		if err := setJavaOverride(root, d, change, result); err != nil {
			return nil, err
		}
	}
	result.Java = d.Server.Java
	return result, nil
}

func setJavaOverride(root string, d *document.Manifest, change JavaChange, result *JavaChangeResult) error {
	before, err := d.Fingerprint()
	if err != nil {
		return err
	}
	override := document.ManifestJava{}
	if d.Server.Java != nil {
		override = *d.Server.Java
	}
	if change.Version != nil {
		override.Version = *change.Version
	}
	if change.Distribution != nil {
		override.Distribution = *change.Distribution
	}
	d.Server.Java = &override
	after, err := d.Fingerprint()
	if err != nil {
		return err
	}
	if after == before {
		log.Debug("java override unchanged", "reason", "lucy.yaml already declares it")
		return nil
	}
	if err := document.ManifestFile(root).Write(d); err != nil {
		return err
	}
	result.PublishedManifest = true
	log.Info(
		"java override changed",
		"version", override.Version,
		"distribution", override.Distribution.String(),
	)
	return nil
}
