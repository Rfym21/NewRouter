package anonymizer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"new-router/pkg/anonymizer/internal/guard"
)

// AstrLink Guard model assets are published under Apache-2.0 at
// https://huggingface.co/QuantumNous/astrlink-guard.
const (
	localModelRevision    = "7216b6a3b897ef903d4b1ba367ae6fb95b0fa5e4"
	localInferenceTimeout = 5 * time.Second
)

var localModelAssets = []struct {
	name   string
	size   int64
	sha256 string
}{
	{"model_int8.onnx", 330781, "0a070d3113615e33ba3b84d6bd486f663a377c04c4b056e7381732179f51832a"},
	{"model_int8.onnx_data", 37124608, "0ba34216ceead01e0ae2dfd8e46d52b67e7aee80b04f722d9b758613a91c0067"},
	{"tokenizer.json", 1502881, "0cf3b235ef125658015a6c8026a5fe6f28088bf825101a990ec18a10200feef4"},
}

// LocalDetector classifies text in-process with the pinned AstrLink Guard model.
// Model assets are supplied locally by the operator and never downloaded at runtime.
type LocalDetector struct {
	model       *guard.Model
	fingerprint [32]byte
	turn        chan struct{}
}

// NewLocalDetector verifies every model asset before parsing its authenticated bytes.
func NewLocalDetector(dir string) (*LocalDetector, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("local anonymization model directory is required")
	}
	assets := make([][]byte, 0, len(localModelAssets))
	for _, asset := range localModelAssets {
		file, err := os.Open(filepath.Join(dir, asset.name))
		if err != nil {
			return nil, fmt.Errorf("local anonymization asset %s is unavailable", asset.name)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, asset.size+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return nil, fmt.Errorf("could not read local anonymization asset %s", asset.name)
		}
		hash := sha256.Sum256(data)
		if int64(len(data)) != asset.size || hex.EncodeToString(hash[:]) != asset.sha256 {
			return nil, fmt.Errorf("local anonymization asset %s failed integrity validation", asset.name)
		}
		assets = append(assets, data)
	}
	model, err := guard.Load(assets[0], assets[1], assets[2])
	if err != nil {
		return nil, errors.New("could not load verified local anonymization model")
	}
	fingerprint := sha256.Sum256([]byte("astrlink-guard:" + localModelRevision + ":newrouter-cpu-v1:threshold=0.8"))
	return &LocalDetector{model: model, fingerprint: fingerprint, turn: make(chan struct{}, guard.Cores())}, nil
}

// Fingerprint identifies the model revision and detection policy for scoped history caches.
func (d *LocalDetector) Fingerprint() [32]byte {
	if d == nil {
		return [32]byte{}
	}
	return d.fingerprint
}

// Detect returns UTF-8 byte spans after complete local inference, or an error on cancellation.
func (d *LocalDetector) Detect(ctx context.Context, text string) ([]Match, error) {
	if d == nil || d.model == nil {
		return nil, errors.New("local anonymization model is unavailable")
	}
	if len(text) > maxPresidioTextBytes || !utf8.ValidString(text) {
		return nil, errors.New("invalid local anonymization text size or encoding")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, localInferenceTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if text == "" {
		return nil, nil
	}
	select {
	case d.turn <- struct{}{}:
		defer func() { <-d.turn }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	spans, err := d.model.Find(ctx, text)
	if err != nil {
		return nil, err
	}
	matches := make([]Match, 0, len(spans))
	for _, span := range spans {
		if span.Score < guard.MinScore {
			continue
		}
		entityType := localEntityType(span.Label)
		if entityType == "" {
			continue
		}
		if span.Start < 0 || span.End > len(text) || span.Start >= span.End || !utf8.ValidString(text[span.Start:span.End]) {
			return nil, errors.New("local anonymization model returned invalid spans")
		}
		matches = append(matches, Match{Text: text[span.Start:span.End], EntityType: entityType, Start: span.Start, End: span.End})
	}
	return matches, ctx.Err()
}

func localEntityType(label string) string {
	switch label {
	case "private_person":
		return "PERSON"
	case "private_address":
		return "ADDRESS"
	case "phone":
		return "PHONE_NUMBER"
	case "email":
		return "EMAIL_ADDRESS"
	case "account", "payment_card":
		return "IDENTIFIER"
	case "common_secret":
		return "CREDENTIAL"
	case "url":
		return "URL"
	case "ip_address":
		return "IP_ADDRESS"
	case "private_date":
		return "DATE_TIME"
	default:
		return ""
	}
}
