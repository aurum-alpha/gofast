package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"
)

// ErrReadOnly is returned when config.yaml cannot be written because the path
// is on a read-only mount or lacks write permission. The API surfaces this so
// the UI can tell the operator to mount the config read-write.
var ErrReadOnly = errors.New("config file is read-only")

// PathOp is one edit to config.yaml addressed by a dotted leaf path
// (e.g. "cache_logos", "health.l1_interval", "providers.lg.label").
// Remove deletes the leaf (reset-to-default); otherwise Value is set.
// Path segments cannot contain dots, so dotted-hostname keys (artwork_tls)
// are not addressable through path ops.
type PathOp struct {
	Path   string `json:"path"`
	Value  any    `json:"value,omitempty"`
	Remove bool   `json:"remove,omitempty"`
}

// ApplyPathOps applies ops to the YAML document in prior and returns the new
// document bytes, preserving comments, key order, and unknown keys on nodes it
// does not touch. Empty/missing prior is an empty document. Set ops create
// intermediate mappings as needed; remove ops delete the leaf and prune
// intermediate mappings that become empty.
func ApplyPathOps(prior []byte, ops []PathOp) ([]byte, error) {
	var doc yaml.Node
	if len(prior) > 0 {
		if err := yaml.Unmarshal(prior, &doc); err != nil {
			return nil, fmt.Errorf("config: parse prior: %w", err)
		}
	}
	root := ensureMappingRoot(&doc)
	for _, op := range ops {
		segments := strings.Split(op.Path, ".")
		for _, s := range segments {
			if strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("config: invalid path %q", op.Path)
			}
		}
		if op.Remove {
			removePath(root, segments)
			continue
		}
		valueNode := &yaml.Node{}
		if err := valueNode.Encode(normalizeValue(op.Value)); err != nil {
			return nil, fmt.Errorf("config: encode %s: %w", op.Path, err)
		}
		if err := setPath(root, segments, valueNode); err != nil {
			return nil, fmt.Errorf("config: %s: %w", op.Path, err)
		}
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, fmt.Errorf("config: marshal: %w", err)
	}
	return out, nil
}

// normalizeValue converts JSON-decoded numbers to integers when integral so
// int-typed config fields round-trip through YAML without a !!float tag.
func normalizeValue(v any) any {
	switch t := v.(type) {
	case float64:
		if t == float64(int64(t)) {
			return int64(t)
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = normalizeValue(e)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = normalizeValue(e)
		}
		return out
	default:
		return v
	}
}

// setPath sets the leaf at segments under root, creating intermediate mappings.
func setPath(root *yaml.Node, segments []string, value *yaml.Node) error {
	node := root
	for _, key := range segments[:len(segments)-1] {
		child := mapValue(node, key)
		if child == nil {
			child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMapKey(node, key, child)
		} else if child.Kind != yaml.MappingNode {
			return fmt.Errorf("segment %q is not a mapping", key)
		}
		node = child
	}
	setMapKey(node, segments[len(segments)-1], value)
	return nil
}

// removePath deletes the leaf at segments and prunes intermediate mappings that
// become empty. A missing path is a no-op.
func removePath(root *yaml.Node, segments []string) {
	chain := make([]*yaml.Node, 0, len(segments))
	node := root
	for _, key := range segments[:len(segments)-1] {
		chain = append(chain, node)
		child := mapValue(node, key)
		if child == nil || child.Kind != yaml.MappingNode {
			return
		}
		node = child
	}
	deleteMapKey(node, segments[len(segments)-1])
	for i := len(chain) - 1; i >= 0; i-- {
		if len(node.Content) > 0 {
			return
		}
		deleteMapKey(chain[i], segments[i])
		node = chain[i]
	}
}

// FileKeys returns the set of dotted leaf paths present in the YAML file at
// path (so the API can report whether a field's value came from the file).
// A missing file yields an empty set.
func FileKeys(path string) (map[string]bool, error) {
	out := map[string]bool{}
	if path == "" {
		return out, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, classify(err))
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if len(doc.Content) > 0 && doc.Content[0].Kind == yaml.MappingNode {
		collectKeys(doc.Content[0], "", out)
	}
	return out, nil
}

func collectKeys(node *yaml.Node, prefix string, out map[string]bool) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		out[path] = true
		if v := node.Content[i+1]; v.Kind == yaml.MappingNode {
			collectKeys(v, path, out)
		}
	}
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

// setMapKey replaces the value for key in a mapping node, or appends it. A
// modified flow-style mapping (e.g. a generated "providers: {}") is switched to
// block style so grown maps stay readable.
func setMapKey(root *yaml.Node, key string, value *yaml.Node) {
	root.Style = 0
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

// mapValue returns the value node for key in a mapping node, or nil.
func mapValue(root *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1]
		}
	}
	return nil
}

// deleteMapKey removes key (and its value) from a mapping node if present.
func deleteMapKey(root *yaml.Node, key string) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			return
		}
	}
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
