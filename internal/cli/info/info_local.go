package info

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
)

func isArtifactPath(root, path string) (bool, error) {
	if !artifact.SupportsPath(path) {
		return false, nil
	}

	resolved := resolveInfoPath(root, path)
	info, err := os.Stat(resolved)
	if err != nil {
		return true, infoArtifactPathFailure(root, path, err)
	}
	if !info.Mode().IsRegular() {
		return true, &report.Detail{
			Title: "artifact is not a regular file: " +
				infoDisplayPath(root, path),
			Lines: []string{"A directory or special file cannot be inspected."},
			Hint:  "Pass a packaged JAR, ZIP, PYZ, or MCDR file.",
			Code:  infoErrorArtifactRead,
		}
	}
	return true, nil
}

func actionArtifactInfo(
	ctx context.Context,
	output *cli.Output,
	inv *cli.Invocation,
	router *routing.Router,
	long bool,
	root string,
	filePath string,
) error {
	if err := ctx.Err(); err != nil {
		return infoCancellation(err)
	}
	infos, err := artifact.ReadInfo(filePath)
	if err != nil {
		return infoArtifactReadFailure(root, filePath, err)
	}
	if len(infos) == 0 {
		return infoArtifactEmptyFailure(root, filePath)
	}

	document, warnings, err := buildArtifactDocument(
		ctx,
		router,
		root,
		filePath,
		infos,
	)
	if err != nil {
		return err
	}
	return infoWriteDocument(ctx, output, inv, document, warnings, long)
}

// artifactLookup is the upstream result for one artifact file. Ref is the
// project that matched the file hash, nil when nothing matched. Info is that
// project's metadata, nil when the metadata lookup failed. Warnings holds the
// problems found during the lookup.
type artifactLookup struct {
	Ref      *catalog.VersionedRef
	Info     *upstream.Info
	Warnings []string
}

func (l artifactLookup) Matched() bool {
	return l.Ref != nil
}

func lookupArtifactUpstream(
	ctx context.Context,
	router *routing.Router,
	filePath string,
) (artifactLookup, error) {
	result := artifactLookup{}
	hashLookup := router.ResolveArtifactByHash(
		ctx,
		artifact.File{Path: filePath},
	)
	if err := ctx.Err(); err != nil {
		return result, infoCancellation(err)
	}

	for _, providerErr := range hashLookup.Errors {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"could not look up this artifact on %s: %v",
			providerErr.Provider.Title(),
			providerErr.Err,
		))
	}
	if !hashLookup.Matched() {
		if hashLookup.Complete() {
			result.Warnings = append(
				result.Warnings,
				"no upstream project matched this artifact hash",
			)
		} else {
			result.Warnings = append(
				result.Warnings,
				"could not determine whether this artifact exists upstream",
			)
		}
		return result, nil
	}

	ref := hashLookup.Ref
	result.Ref = &ref

	providers, err := router.ResolveInfoProviders(ref.Ecosystem, ref.Provider)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"cannot retrieve upstream metadata for %s: %v",
			ref.StringBase(),
			err,
		))
		return result, nil
	}

	info, providerErrors, lookupErr := router.GetInfoFirstSuccess(
		ctx,
		providers,
		ref.Ref,
	)
	if err := ctx.Err(); err != nil {
		return result, infoCancellation(err)
	}
	for _, providerErr := range providerErrors {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"could not retrieve metadata from %s: %v",
			providerErr.Provider.Title(),
			providerErr.Err,
		))
	}
	if lookupErr != nil {
		if failure := infoOperationFailure(ctx, lookupErr); failure != nil {
			return result, failure
		}
		if len(providerErrors) == 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"cannot retrieve upstream metadata for %s: %v",
				ref.StringBase(),
				lookupErr,
			))
		}
		return result, nil
	}
	result.Info = &info
	return result, nil
}

func buildArtifactDocument(
	ctx context.Context,
	router *routing.Router,
	root string,
	filePath string,
	infos []artifact.PackageInfo,
) (infoDocument, []string, error) {
	lookup, err := lookupArtifactUpstream(ctx, router, filePath)
	if err != nil {
		return infoDocument{}, nil, err
	}

	document := newInfoDocument(
		infoKindArtifact,
		infoDisplayPath(root, filePath),
	)
	warnings := append([]string{}, lookup.Warnings...)
	single := len(infos) == 1
	attributed := 0
	for _, info := range infos {
		upstreamRef, match := infoArtifactUpstream(info, single, lookup)
		if upstreamRef != nil {
			attributed++
		}
		document.Entries = append(
			document.Entries,
			infoArtifactEntry(info, upstreamRef, match),
		)
	}
	if lookup.Ref != nil && !single && attributed == 0 {
		warnings = append(warnings, fmt.Sprintf(
			"the upstream hash match %s does not name any package in this archive; upstream details were not applied",
			lookup.Ref.String(),
		))
	}
	return document, warnings, nil
}

func infoArtifactPathFailure(root, path string, err error) *report.Detail {
	label := infoDisplayPath(root, path)
	if errors.Is(err, os.ErrNotExist) {
		return &report.Detail{
			Title: "artifact not found: " + label,
			Lines: []string{infoCleanLine(err.Error())},
			Hint:  "Check the path, or pass a package reference instead.",
			Code:  infoErrorArtifactMiss,
			Cause: err,
		}
	}
	return &report.Detail{
		Title: "cannot inspect artifact: " + label,
		Lines: []string{infoCleanLine(err.Error())},
		Hint:  "Check that the file is readable.",
		Code:  infoErrorArtifactRead,
		Cause: err,
	}
}

func infoArtifactReadFailure(root, path string, err error) *report.Detail {
	return &report.Detail{
		Title: "cannot read artifact: " + infoDisplayPath(root, path),
		Lines: []string{infoCleanLine(err.Error())},
		Hint:  "Pass a readable JAR, ZIP, PYZ, or MCDR file.",
		Code:  infoErrorArtifactRead,
		Cause: err,
	}
}

func infoArtifactEmptyFailure(root, path string) *report.Detail {
	return &report.Detail{
		Title: "no package information in " + infoDisplayPath(root, path),
		Lines: []string{
			"The archive contains no descriptor Lucy can read.",
		},
		Hint: "Check that the file is a packaged mod or plugin.",
		Code: infoErrorArtifactEmpty,
	}
}

// infoDisplayPath keeps the document's file field and error labels stable
// without exposing an absolute workspace path.
func infoDisplayPath(root, path string) string {
	return infoCleanPath(terminal.DisplayPath(root, resolveInfoPath(root, path)))
}

// resolveInfoPath anchors a user-supplied artifact path at the invocation
// directory; absolute paths pass through unchanged.
func resolveInfoPath(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, path)
}
