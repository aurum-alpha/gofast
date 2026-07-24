package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"gopkg.in/yaml.v3"
)

// ErrReadOnly is returned when config.yaml cannot be written because the path
// is on a read-only mount or lacks write permission. The API surfaces this so
// the UI can tell the operator to mount the config read-write.
var ErrReadOnly = errors.New("config file is read-only")

// WriteMapKey sets a single top-level mapping key in the YAML file at path to
// value, preserving comments, key order, and unknown keys the operator added.
// The write is atomic (temp file + rename) and keeps a .bak of the prior bytes.
// A missing or empty file is treated as an empty document. A read-only target
// returns ErrReadOnly.
func WriteMapKey(path, key string, value any) error {
	if path == "" {
		return errors.New("config: no path")
	}
	prior, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("config: read %s: %w", path, classify(err))
	}

	var doc yaml.Node
	if len(prior) > 0 {
		if err := yaml.Unmarshal(prior, &doc); err != nil {
			return fmt.Errorf("config: parse %s: %w", path, err)
		}
	}
	root := ensureMappingRoot(&doc)

	valueNode := &yaml.Node{}
	if err := valueNode.Encode(value); err != nil {
		return fmt.Errorf("config: encode %s: %w", key, err)
	}
	setMapKey(root, key, valueNode)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	return atomicWriteWithBackup(path, out, prior)
}

// WriteDefault writes the baked-in code defaults to path when the file does not
// exist, so first boot can proceed with an operator-editable config.yaml.
// Deploy-varying values stay in the environment (not baked into the file); it
// never overwrites an existing file. A read-only target returns ErrReadOnly.
func WriteDefault(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("config: stat %s: %w", path, classify(err))
	}
	out, err := yaml.Marshal(defaults())
	if err != nil {
		return fmt.Errorf("config: marshal defaults: %w", err)
	}
	return atomicWriteWithBackup(path, out, nil)
}

// ensureMappingRoot returns the mapping node at the document root, creating the
// document/mapping structure when the input was empty.
func ensureMappingRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		root.Kind = yaml.MappingNode
		root.Tag = "!!map"
		root.Content = nil
	}
	return root
}

// setMapKey replaces the value for key in a mapping node, or appends it.
func setMapKey(root *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content[i+1] = value
			return
		}
	}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

// atomicWriteWithBackup writes data via temp+rename, backing up prior bytes to
// path+".bak" first. A read-only/permission error is mapped to ErrReadOnly.
func atomicWriteWithBackup(path string, data, prior []byte) error {
	dir := filepath.Dir(path)
	if len(prior) > 0 {
		if err := os.WriteFile(path+".bak", prior, 0o644); err != nil {
			return fmt.Errorf("config: write backup: %w", classify(err))
		}
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("config: temp: %w", classify(err))
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("config: write %s: %w", path, classify(err))
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("config: sync %s: %w", path, classify(err))
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: close %s: %w", path, classify(err))
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: rename %s: %w", path, classify(err))
	}
	return nil
}

// ProbeWritable reports whether config.yaml could be written, returning
// ErrReadOnly for a read-only mount / permission denial without modifying the
// file. It is a best-effort hint for the UI, not a guarantee.
func ProbeWritable(path string) error {
	if path == "" {
		return ErrReadOnly
	}
	if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		f.Close()
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return classify(err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return classify(err)
	}
	name := tmp.Name()
	tmp.Close()
	os.Remove(name)
	return nil
}

// classify maps read-only / permission errors to ErrReadOnly (joined so callers
// can errors.Is either the OS error or ErrReadOnly), leaving others untouched.
func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EROFS) || errors.Is(err, os.ErrPermission) {
		return errors.Join(ErrReadOnly, err)
	}
	return err
}
