package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

// channelHostNode is one reverse-DNS label in the hosts tree (TLD at the root).
type channelHostNode struct {
	Label    string            `json:"label"`
	Count    int               `json:"count"`
	Children []channelHostNode `json:"children"`
}

type channelHostsResponse struct {
	URLField string            `json:"url_field"`
	Tree     []channelHostNode `json:"tree"`
	Unparsed int               `json:"unparsed"`
}

type hostTrieNode struct {
	children map[string]*hostTrieNode
	count    int
}

// ChannelHostsHandler serves GET /api/channels/hosts — per-request reverse-DNS
// tree of live lineup stream_url hosts (TLD → domain → subdomain…).
func ChannelHostsHandler(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		out := aggregateChannelHosts(reg.Channels())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

func aggregateChannelHosts(chs []model.Channel) channelHostsResponse {
	root := &hostTrieNode{children: make(map[string]*hostTrieNode)}
	unparsed := 0
	for _, ch := range chs {
		host, ok := streamURLHost(ch.StreamURL)
		if !ok {
			unparsed++
			continue
		}
		insertHostPath(root, hostLabelPath(host))
	}
	return channelHostsResponse{
		URLField: "stream_url",
		Tree:     materializeHostTrie(root),
		Unparsed: unparsed,
	}
}

// hostLabelPath returns DNS labels in reverse order (TLD first).
// dai.google.com → ["com", "google", "dai"]. IPs stay a single leaf.
func hostLabelPath(host string) []string {
	if net.ParseIP(host) != nil {
		return []string{host}
	}
	parts := strings.Split(host, ".")
	out := make([]string, 0, len(parts))
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func insertHostPath(root *hostTrieNode, path []string) {
	if len(path) == 0 {
		return
	}
	n := root
	for _, label := range path {
		child, ok := n.children[label]
		if !ok {
			child = &hostTrieNode{children: make(map[string]*hostTrieNode)}
			n.children[label] = child
		}
		child.count++
		n = child
	}
}

func materializeHostTrie(root *hostTrieNode) []channelHostNode {
	out := make([]channelHostNode, 0, len(root.children))
	for label, child := range root.children {
		out = append(out, channelHostNode{
			Label:    label,
			Count:    child.count,
			Children: materializeHostTrie(child),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	return out
}

func streamURLHost(raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", false
	}
	return host, true
}
