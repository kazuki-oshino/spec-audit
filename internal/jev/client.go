package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kazuki-oshino/spec-audit/internal/model"
)

const Endpoint = "https://api.typesafe.ai/v1/systemone"

type Client struct {
	HTTP        *http.Client
	URL         string
	APIKey      string
	Model       string
	Concurrency int
}

func (c Client) assess(ctx context.Context, requirement model.Requirement, blocks []model.Block) ([]model.Pair, json.RawMessage, error) {
	if c.APIKey == "" {
		return nil, nil, errors.New("TYPESAFE_API_KEY is not set")
	}
	url := c.URL
	if url == "" {
		url = Endpoint
	}
	modelID := c.Model
	if modelID == "" {
		modelID = "jev-latest"
	}
	state := map[string]any{"requirement": requirement, "design_blocks": blocks}
	questions := make(map[string]any)
	for i, block := range blocks {
		prefix := fmt.Sprintf("b%d_", i)
		questions[prefix+"related"] = map[string]any{"type": "noul", "instructions": fmt.Sprintf("Does requirement `requirement.statement`, with `requirement.conditions`, address the same subject and conditions as design block %q in `design_blocks`?", block.ID)}
		questions[prefix+"supports"] = map[string]any{"type": "noul", "instructions": fmt.Sprintf("Does design block %q explicitly satisfy all or a meaningful part of `requirement.statement` under `requirement.conditions`? Absence is no.", block.ID)}
		questions[prefix+"contradicts"] = map[string]any{"type": "noul", "instructions": fmt.Sprintf("Does design block %q explicitly prescribe behavior incompatible with `requirement.statement` under the same conditions? Absence or ambiguity is no.", block.ID)}
	}
	body, err := json.Marshal(map[string]any{"state": state, "model": modelID, "questions": questions})
	if err != nil {
		return nil, nil, err
	}
	if len(body) > 32*1024 {
		return nil, nil, errors.New("Jev batch exceeds 32 KiB; split design blocks")
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	var raw []byte
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		raw, err = io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
		_ = resp.Body.Close()
		if err != nil {
			return nil, nil, err
		}
		if len(raw) > 4*1024*1024 {
			return nil, nil, errors.New("Jev response too large")
		}
		if resp.StatusCode == 200 {
			break
		}
		if (resp.StatusCode == 429 || resp.StatusCode >= 500) && attempt < 2 {
			wait := time.Duration(1<<attempt) * time.Second
			if value, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && value >= 0 && value <= 30 {
				wait = time.Duration(value) * time.Second
			}
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}
		return nil, nil, fmt.Errorf("Jev HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		Model   string `json:"model"`
		Answers map[string]struct {
			Type string  `json:"type"`
			Noul float64 `json:"noul"`
		} `json:"answers"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, nil, err
	}
	if parsed.Model == "" {
		return nil, nil, errors.New("Jev response has no model")
	}
	if len(parsed.Answers) != len(questions) {
		return nil, nil, errors.New("Jev answer count mismatch")
	}
	value := func(key string) (float64, error) {
		a, ok := parsed.Answers[key]
		if !ok || a.Type != "noul" || math.IsNaN(a.Noul) || math.IsInf(a.Noul, 0) || a.Noul < 0 || a.Noul > 1 {
			return 0, fmt.Errorf("invalid Jev answer %q", key)
		}
		return a.Noul, nil
	}
	pairs := make([]model.Pair, 0, len(blocks))
	for i, block := range blocks {
		prefix := fmt.Sprintf("b%d_", i)
		related, err := value(prefix + "related")
		if err != nil {
			return nil, nil, err
		}
		supports, err := value(prefix + "supports")
		if err != nil {
			return nil, nil, err
		}
		contradicts, err := value(prefix + "contradicts")
		if err != nil {
			return nil, nil, err
		}
		pairs = append(pairs, model.Pair{RequirementID: requirement.ID, DesignBlockID: block.ID, Related: related, Supports: supports, Contradicts: contradicts, Model: parsed.Model})
	}
	return pairs, parsed.Usage, nil
}

func (c Client) Assess(ctx context.Context, requirement model.Requirement, blocks []model.Block) ([]model.Pair, []json.RawMessage, error) {
	var batches [][]model.Block
	for start := 0; start < len(blocks); {
		end := start + 8
		if end > len(blocks) {
			end = len(blocks)
		}
		for end > start+1 {
			body, _ := json.Marshal(blocks[start:end])
			if len(body) <= 12*1024 {
				break
			}
			end--
		}
		batches = append(batches, blocks[start:end])
		start = end
	}
	results := make([][]model.Pair, len(batches))
	usages := make([]json.RawMessage, len(batches))
	jobs := make(chan int)
	workers := c.Concurrency
	if workers <= 0 {
		workers = 1
	}
	if workers > len(batches) {
		workers = len(batches)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var firstErr error
	var once sync.Once
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				pairs, usage, err := c.assess(ctx, requirement, batches[index])
				if err != nil {
					once.Do(func() { firstErr = err; cancel() })
					continue
				}
				results[index], usages[index] = pairs, usage
			}
		}()
	}
	for i := range batches {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	var all []model.Pair
	for _, pairs := range results {
		all = append(all, pairs...)
	}
	if firstErr != nil {
		return all, usages, firstErr
	}
	if err := ctx.Err(); err != nil {
		return all, usages, err
	}
	return all, usages, nil
}

func FromEnvironment(modelID string) Client {
	return Client{APIKey: os.Getenv("TYPESAFE_API_KEY"), Model: modelID}
}
