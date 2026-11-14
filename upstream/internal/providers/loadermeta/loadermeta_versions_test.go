package loadermeta

import (
	"testing"

	"github.com/mclucy/lucy/version"
)

func TestSelectLoaderVersionStableRejectsPrereleases(t *testing.T) {
	// The live list is not grouped by channel: prerelease builds sit above and
	// between stable releases. Stable selection must skip every prerelease.
	versions := []loaderVersionEntry{
		{Version: "0.31.0-beta.6"},
		{Version: "0.30.2"},
		{Version: "0.31.0-beta.5"},
		{Version: "0.30.2-beta.1"},
		{Version: "0.30.1"},
		{Version: "0.30.1-beta.4"},
		{Version: "0.29.1"},
	}

	got, err := selectLoaderVersion(versions, false, version.Stable)
	if err != nil {
		t.Fatalf("select stable loader version: %v", err)
	}
	if got.Version != "0.30.2" {
		t.Fatalf("stable loader = %q, want 0.30.2", got.Version)
	}
}

func TestSelectLoaderVersionStableUsesStableFlag(t *testing.T) {
	versions := []loaderVersionEntry{
		{Version: "0.17.0-beta.1", Stable: false},
		{Version: "0.16.9", Stable: true},
		{Version: "0.16.8", Stable: true},
	}

	got, err := selectLoaderVersion(versions, true, version.Stable)
	if err != nil {
		t.Fatalf("select stable fabric loader version: %v", err)
	}
	if got.Version != "0.16.9" {
		t.Fatalf("stable loader = %q, want 0.16.9", got.Version)
	}
}

func TestSelectLoaderVersionStableWithoutStableRelease(t *testing.T) {
	versions := []loaderVersionEntry{
		{Version: "0.31.0-beta.6"},
		{Version: "0.31.0-beta.5"},
		{Version: "0.30.2-beta.1"},
	}

	if got, err := selectLoaderVersion(
		versions,
		false,
		version.Stable,
	); err == nil {
		t.Fatalf("stable loader = %q, want an error", got.Version)
	}
}

func TestSelectLoaderVersionExplicit(t *testing.T) {
	versions := []loaderVersionEntry{
		{Version: "0.31.0-beta.6"},
		{Version: "0.30.2"},
		{Version: "0.30.1-beta.4"},
		{Version: "0.29.1"},
	}

	for _, want := range []string{
		"0.31.0-beta.6",
		"0.30.2",
		"0.30.1-beta.4",
		"0.29.1",
	} {
		t.Run(want, func(t *testing.T) {
			got, err := selectLoaderVersion(
				versions,
				false,
				version.Version(want),
			)
			if err != nil {
				t.Fatalf("select explicit loader %q: %v", want, err)
			}
			if got.Version != want {
				t.Fatalf("explicit loader = %q, want %q", got.Version, want)
			}
		})
	}
}

func TestSelectLoaderVersionExplicitUnknown(t *testing.T) {
	versions := []loaderVersionEntry{
		{Version: "0.30.2"},
		{Version: "0.29.1"},
	}

	if got, err := selectLoaderVersion(
		versions,
		false,
		version.Version("0.30.0"),
	); err == nil {
		t.Fatalf("explicit loader = %q, want an error", got.Version)
	}
}

func TestSelectLoaderVersionFloatingUsesNewestEntry(t *testing.T) {
	versions := []loaderVersionEntry{
		{Version: "0.31.0-beta.6"},
		{Version: "0.30.2"},
		{Version: "0.29.1"},
	}

	for _, selector := range []version.Version{version.Any, version.Beta} {
		t.Run(selector.String(), func(t *testing.T) {
			got, err := selectLoaderVersion(versions, false, selector)
			if err != nil {
				t.Fatalf("select %s loader version: %v", selector, err)
			}
			if got.Version != "0.31.0-beta.6" {
				t.Fatalf(
					"%s loader = %q, want newest entry 0.31.0-beta.6",
					selector,
					got.Version,
				)
			}
		})
	}
}

func TestSelectLoaderVersionRejectsEmptyListAndInvalidSelector(t *testing.T) {
	if got, err := selectLoaderVersion(
		nil,
		false,
		version.Stable,
	); err == nil {
		t.Fatalf("empty loader metadata = %q, want an error", got.Version)
	}
	for _, selector := range []version.Version{version.Unknown, version.None, ""} {
		if got, err := selectLoaderVersion(
			[]loaderVersionEntry{{Version: "0.30.2"}},
			false,
			selector,
		); err == nil {
			t.Fatalf("selector %q = %q, want an error", selector, got.Version)
		}
	}
}

func TestSelectGameVersion(t *testing.T) {
	versions := []gameVersionEntry{
		{Version: "1.21.2-pre1", Stable: false},
		{Version: "1.21.1", Stable: true},
		{Version: "1.21", Stable: true},
		{Version: "1.20.6", Stable: true},
	}

	stable, err := selectGameVersion(versions, version.Stable)
	if err != nil {
		t.Fatalf("select stable game version: %v", err)
	}
	if stable.Version != "1.21.1" {
		t.Fatalf("stable game = %q, want 1.21.1", stable.Version)
	}

	for _, selector := range []version.Version{version.Any, version.Beta} {
		got, err := selectGameVersion(versions, selector)
		if err != nil {
			t.Fatalf("select %s game version: %v", selector, err)
		}
		if got.Version != "1.21.2-pre1" {
			t.Fatalf("%s game = %q, want newest entry", selector, got.Version)
		}
	}

	explicit, err := selectGameVersion(
		versions,
		version.Version("1.20.6"),
	)
	if err != nil {
		t.Fatalf("select explicit game version: %v", err)
	}
	if explicit.Version != "1.20.6" {
		t.Fatalf("explicit game = %q, want 1.20.6", explicit.Version)
	}

	if got, err := selectGameVersion(
		versions,
		version.Version("1.21.3"),
	); err == nil {
		t.Fatalf("unknown game = %q, want an error", got.Version)
	}
}
