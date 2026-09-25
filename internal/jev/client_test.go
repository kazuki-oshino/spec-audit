package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kazuki-oshino/spec-audit/internal/model"
)

func TestAssessValidatesEveryAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fake" {
			t.Error("missing token")
		}
		var body struct {
			Questions map[string]json.RawMessage `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		answers := map[string]any{}
		for key := range body.Questions {
			answers[key] = map[string]any{"type": "noul", "noul": 0.8}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "jev-test", "answers": answers, "usage": map[string]int{"input_tokens": 1}})
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), URL: server.URL, APIKey: "fake", Model: "jev-test"}
	blocks := []model.Block{{ID: "d1", Text: "利用者は検索できる。"}, {ID: "d2", Text: "条件付き検索。"}}
	pairs, _, err := client.Assess(context.Background(), model.Requirement{ID: "r1", Statement: "検索できる"}, blocks)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 || pairs[0].Supports != 0.8 || pairs[1].Model != "jev-test" {
		t.Fatalf("pairs=%+v", pairs)
	}
}
