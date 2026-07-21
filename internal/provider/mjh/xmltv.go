package mjh

import (
	"bytes"
	"compress/gzip"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

func decodeGuide(data []byte, known map[string]struct{}) ([]model.Programme, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	document, err := xmltv.Parse(gz)
	if err != nil {
		return nil, err
	}
	programmes := make([]model.Programme, 0, len(document.Programmes))
	skipped := 0
	for _, programme := range document.Programmes {
		if _, ok := known[programme.ChannelID]; !ok {
			provider.SkipMalformed(&skipped)
			continue
		}
		programmes = append(programmes, programme)
	}
	return programmes, nil
}
