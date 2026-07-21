package published

import (
	"net/http"

	"github.com/j27-aurum/gofast/internal/model"
)

func cloneHeaders(headers map[string]string) map[string]string {
	result := make(map[string]string, len(headers))
	for key, value := range headers {
		result[key] = value
	}
	return result
}

func guideMatch(channels []model.Channel, guideIDs []string) (int, float64) {
	if len(channels) == 0 {
		return 0, 0
	}
	guide := make(map[string]struct{}, len(guideIDs))
	for _, id := range guideIDs {
		if normalized := model.NormalizeID(id); normalized != "" {
			guide[normalized] = struct{}{}
		}
	}
	matched := 0
	for _, channel := range channels {
		if _, ok := guide[model.NormalizeID(channel.ID)]; ok {
			matched++
		}
	}
	return matched, float64(matched) / float64(len(channels))
}

func makeHeaders(userAgent string, configured map[string]string) map[string]string {
	headers := make(map[string]string, len(configured)+1)
	if userAgent != "" {
		headers[http.CanonicalHeaderKey("User-Agent")] = userAgent
	}
	for key, value := range configured {
		headers[http.CanonicalHeaderKey(key)] = value
	}
	return headers
}
