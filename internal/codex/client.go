package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Client struct {
	Command []string
	Root    string
	CWD     string
	Model   string
	Effort  string
	Timeout time.Duration
}

type Request struct {
	Version   int    `json:"version"`
	RequestID string `json:"request_id"`
	Task      string `json:"task"`
	Model     string `json:"model,omitempty"`
	Effort    string `json:"effort,omitempty"`
	CWD       string `json:"cwd"`
	Payload   any    `json:"payload"`
}

type Response struct {
	Version   int             `json:"version"`
	RequestID string          `json:"request_id"`
	OK        bool            `json:"ok"`
	Result    json.RawMessage `json:"result"`
	Usage     json.RawMessage `json:"usage"`
	Error     struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

type boundedBuffer struct {
	bytes.Buffer
	Limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.Limit {
		return 0, errors.New("bridge output limit exceeded")
	}
	return b.Buffer.Write(p)
}

func (c Client) Call(ctx context.Context, id, task string, payload any, into any) (json.RawMessage, error) {
	if len(c.Command) == 0 {
		return nil, errors.New("bridge command is empty")
	}
	cmdCtx := ctx
	var cancel context.CancelFunc
	if c.Timeout > 0 {
		cmdCtx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	command := append([]string(nil), c.Command...)
	if len(command) > 1 && !filepath.IsAbs(command[1]) && (filepath.Ext(command[1]) == ".js" || filepath.Ext(command[1]) == ".mjs") {
		command[1] = filepath.Join(c.Root, command[1])
	}
	cmd := exec.CommandContext(cmdCtx, command[0], command[1:]...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 3 * time.Second
	cmd.Dir = c.CWD
	request := Request{Version: 1, RequestID: id, Task: task, Model: c.Model, Effort: c.Effort, CWD: c.CWD, Payload: payload}
	input, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	cmd.Stdin = bytes.NewReader(input)
	stdout := &boundedBuffer{Limit: 16 * 1024 * 1024}
	stderr := &boundedBuffer{Limit: 1024 * 1024}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err = cmd.Run()
	if cmdCtx.Err() != nil {
		return nil, cmdCtx.Err()
	}
	if stdout.Len() == 0 {
		if err != nil {
			return nil, fmt.Errorf("bridge process: %w: %s", err, stderr.String())
		}
		return nil, errors.New("empty bridge response")
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	var response Response
	if decodeErr := decoder.Decode(&response); decodeErr != nil {
		return nil, fmt.Errorf("bridge response: %w", decodeErr)
	}
	var extra any
	if decodeErr := decoder.Decode(&extra); decodeErr != io.EOF {
		return nil, errors.New("bridge emitted more than one JSON object")
	}
	if response.Version != 1 || response.RequestID != id {
		return nil, errors.New("bridge response version or request ID mismatch")
	}
	if !response.OK {
		return nil, fmt.Errorf("bridge %s: %s", response.Error.Code, response.Error.Message)
	}
	if err != nil {
		return nil, fmt.Errorf("bridge exited after success: %w", err)
	}
	if len(response.Result) == 0 {
		return nil, errors.New("bridge result missing")
	}
	if err := json.Unmarshal(response.Result, into); err != nil {
		return nil, fmt.Errorf("bridge result: %w", err)
	}
	return response.Usage, nil
}

func CheckCommand(command []string, root string) error {
	if len(command) == 0 {
		return errors.New("bridge command is empty")
	}
	if _, err := exec.LookPath(command[0]); err != nil {
		return err
	}
	if len(command) > 1 && (filepath.Ext(command[1]) == ".js" || filepath.Ext(command[1]) == ".mjs") {
		path := command[1]
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("bridge %s is missing; run just build-bridge: %w", path, err)
		}
	}
	return nil
}
