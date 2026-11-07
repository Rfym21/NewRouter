// Copyright (c) 2026 yetone. SPDX-License-Identifier: MIT.
// Adapted from github.com/yetone/magpie/internal/pii/guard at
// 05f6fd423bc5bb6f1f957a3728f29c34f7ccab53. See LICENSE.

// Package guard evaluates the pinned AstrLink Guard int8 token classifier on CPU.
package guard

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"sync/atomic"
)

var labels = []string{"O",
	"B-email", "I-email", "B-phone", "I-phone", "B-account", "I-account",
	"B-payment_card", "I-payment_card", "B-ip_address", "I-ip_address",
	"B-url", "I-url", "B-common_secret", "I-common_secret",
	"B-private_address", "I-private_address", "B-private_date", "I-private_date",
	"B-private_person", "I-private_person"}

const (
	window  = maxPos - 2
	overlap = 128
)

// Model owns immutable weights and tokenizer state shared by concurrent requests.
type Model struct {
	tok *tokenizer
	net *net
}

// Span identifies a UTF-8 byte range and its least confident token probability.
type Span struct {
	Start, End int
	Label      string
	Score      float64
}

// MinScore is the model publisher's default entity confidence threshold.
const MinScore = 0.8

// Cores bounds total inference parallelism independently of concurrent requests.
func Cores() int {
	return max(1, min(4, runtime.GOMAXPROCS(0)/2))
}

var cores = make(chan struct{}, Cores())

// Load parses already authenticated assets without reopening operator paths.
func Load(model, external, tokenizer []byte) (*Model, error) {
	tok, err := loadTokenizer(tokenizer)
	if err != nil {
		return nil, fmt.Errorf("tokenizer: %w", err)
	}
	nt, err := loadNet(model, external)
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	if nt.labels != len(labels) {
		return nil, fmt.Errorf("model has %d labels, expected %d", nt.labels, len(labels))
	}
	return &Model{tok: tok, net: nt}, nil
}

func take(ctx context.Context) (int, error) {
	select {
	case cores <- struct{}{}:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	count := 1
	for count < cap(cores) {
		select {
		case cores <- struct{}{}:
			count++
		default:
			return count, nil
		}
	}
	return count, nil
}

func give(count int) {
	for range count {
		<-cores
	}
}

// Find labels overlapping token windows and resolves overlaps by interior margin.
// Cancellation is checked while waiting for CPU capacity and between encoder layers.
func (m *Model) Find(ctx context.Context, text string) ([]Span, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tokens := m.tok.encode(text)
	if len(tokens) == 0 {
		return nil, nil
	}
	lab := make([]int, len(tokens))
	probability := make([]float64, len(tokens))
	margin := make([]int, len(tokens))
	for index := range margin {
		margin[index] = -1
	}
	for start := 0; start < len(tokens); start += window - overlap {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(start+window, len(tokens))
		ids := make([]int, 0, end-start+2)
		ids = append(ids, 0)
		for _, token := range tokens[start:end] {
			ids = append(ids, token.id)
		}
		ids = append(ids, 2)
		procs, err := take(ctx)
		if err != nil {
			return nil, err
		}
		logits, err := func() ([]float32, error) {
			defer give(procs)
			return m.net.logits(ctx, ids, procs)
		}()
		if err != nil {
			return nil, err
		}
		for offset := 0; offset < end-start; offset++ {
			index := start + offset
			interior := min(offset, end-start-1-offset)
			if interior <= margin[index] {
				continue
			}
			row := logits[(offset+1)*len(labels) : (offset+2)*len(labels)]
			best, maximum := 0, row[0]
			for candidate, score := range row {
				if score > maximum {
					best, maximum = candidate, score
				}
			}
			var sum float64
			for _, score := range row {
				sum += math.Exp(float64(score - maximum))
			}
			margin[index], lab[index], probability[index] = interior, best, 1/sum
		}
		if end == len(tokens) {
			break
		}
	}
	return decode(tokens, lab, probability), ctx.Err()
}

// carried propagates worker panics to the calling request instead of terminating
// the process from a detached matrix worker.
type carried struct{ p atomic.Pointer[any] }

func (c *carried) catch() {
	if value := recover(); value != nil {
		c.p.CompareAndSwap(nil, &value)
	}
}

func (c *carried) rethrow() {
	if value := c.p.Load(); value != nil {
		panic(*value)
	}
}

func decode(tokens []token, lab []int, probability []float64) []Span {
	var spans []Span
	current := -1
	for index, token := range tokens {
		if token.start == token.end {
			continue
		}
		tag := labels[lab[index]]
		if tag == "O" {
			current = -1
			continue
		}
		kind := tag[2:]
		if tag[0] == 'B' || current < 0 || spans[current].Label != kind {
			spans = append(spans, Span{token.start, token.end, kind, probability[index]})
			current = len(spans) - 1
			continue
		}
		spans[current].End = token.end
		spans[current].Score = min(spans[current].Score, probability[index])
	}
	return spans
}
