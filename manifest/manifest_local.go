package manifest

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/log"
	"gopkg.in/yaml.v3"
)

// LocalFilename names the per-machine settings next to lucy.yaml. Its values
// are true only on one machine, so the file must stay out of version control
// and exports.
const LocalFilename = "lucy.local.yaml"

// Local holds selections that apply only on this machine. They take precedence
// over the shared manifest.
type Local struct {
	Java *LocalJava `yaml:"java,omitempty" json:"java,omitempty"`
}

// LocalJava selects one Java installation on this machine.
type LocalJava struct {
	// Home is the Java home directory: absolute, or relative to the directory
	// that contains lucy.yaml.
	Home string `yaml:"home" json:"home"`
}

// IsEmpty reports whether l selects nothing.
func (l *Local) IsEmpty() bool {
	return l == nil || l.Java == nil
}

// JavaHome returns the selected Java home as an absolute path, or "" when no
// Java is selected.
func (l *Local) JavaHome(root string) string {
	if l == nil || l.Java == nil {
		return ""
	}
	if filepath.IsAbs(l.Java.Home) {
		return l.Java.Home
	}
	return filepath.Join(root, l.Java.Home)
}

// ReadLocal reads the local settings of root. A missing file selects nothing.
func ReadLocal(root string) (*Local, error) {
	file, err := os.Open(filepath.Join(root, LocalFilename))
	if errors.Is(err, os.ErrNotExist) {
		return &Local{}, nil
	}
	if err != nil {
		return nil, err
	}
	l, decodeErr := decodeLocal(file)
	return l, errors.Join(decodeErr, file.Close())
}

func decodeLocal(reader io.Reader) (*Local, error) {
	l := &Local{}
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	if err := decoder.Decode(l); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode %s: %w", LocalFilename, err)
	}
	if l.Java != nil {
		l.Java.Home = strings.TrimSpace(l.Java.Home)
		if l.Java.Home == "" {
			return nil, fmt.Errorf("%s: java.home is required", LocalFilename)
		}
	}
	return l, nil
}

// WriteLocal publishes l as the local settings of root. Settings that select
// nothing remove the file.
func WriteLocal(root string, l *Local) error {
	path := filepath.Join(root, LocalFilename)
	if l.IsEmpty() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		log.Info("local settings removed", "path", path)
		return nil
	}
	data, err := yaml.Marshal(l)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(path, data, 0o644); err != nil {
		return err
	}
	log.Info("local settings written", "path", path)
	return nil
}
