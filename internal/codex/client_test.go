package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestClientUsesSingleJSONRequestAndValidatesID(t *testing.T) {
	client := Client{Command: []string{"sh", "-c", `input=$(cat); case "$input" in *\"request_id\":\"r1\"*\"model\":\"gpt-6-sol\"*\"effort\":\"medium\"*) printf '%s\n' '{"version":1,"request_id":"r1","ok":true,"result":{"value":"ok"},"usage":{}}';; *) exit 7;; esac`}, CWD: t.TempDir(), Model: "gpt-6-sol", Effort: "medium"}
	var result struct {
		Value string `json:"value"`
	}
	if _, err := client.Call(context.Background(), "r1", "test", map[string]any{"x": 1}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Value != "ok" {
		t.Fatalf("result=%+v", result)
	}
	client.Command = []string{"sh", "-c", `cat >/dev/null; printf '%s\n' '{"version":1,"request_id":"other","ok":true,"result":{},"usage":{}}'`}
	var mismatch json.RawMessage
	_, err := client.Call(context.Background(), "r1", "test", nil, &mismatch)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("expected request mismatch, got %v", err)
	}
}
