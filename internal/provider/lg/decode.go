package lg

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"io"
)

// decodeScheduleBytes normalizes an LG schedulelist body to JSON.
//
// Live responses (observed 2026-07) are base64(zlib(JSON)) with
// Content-Type: text/plain and no Content-Encoding. Older caches and
// fixtures are plain JSON. Accept: application/json is rejected (406).
func decodeScheduleBytes(body []byte) ([]byte, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, fmt.Errorf("lg: empty schedule body")
	}
	if body[0] == '{' || body[0] == '[' {
		return body, nil
	}

	if decoded, err := base64.StdEncoding.DecodeString(string(body)); err == nil {
		if out, err := inflate(decoded); err == nil {
			return out, nil
		}
	}
	if out, err := inflate(body); err == nil {
		return out, nil
	}
	return nil, fmt.Errorf("lg: unrecognized schedule encoding (want JSON or base64+zlib)")
}

func inflate(data []byte) ([]byte, error) {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
