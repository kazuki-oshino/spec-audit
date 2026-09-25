package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/kazuki-oshino/spec-audit/internal/audit"
	"github.com/kazuki-oshino/spec-audit/internal/codex"
	"github.com/kazuki-oshino/spec-audit/internal/config"
	"github.com/kazuki-oshino/spec-audit/internal/jev"
	"github.com/kazuki-oshino/spec-audit/internal/model"
	"github.com/kazuki-oshino/spec-audit/internal/report"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: specaudit run --config audit.yaml | replay --run RUN_DIR")
		return 1
	}
	switch args[0] {
	case "run":
		flags := flag.NewFlagSet("run", flag.ContinueOnError)
		path := flags.String("config", "audit.yaml", "audit configuration")
		if err := flags.Parse(args[1:]); err != nil {
			return 1
		}
		cfg, err := config.Load(*path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := codex.CheckCommand(cfg.LLM.BridgeCommand, cfg.Root); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		jevClient := jev.FromEnvironment(cfg.Jev.Model)
		if jevClient.APIKey == "" {
			fmt.Fprintln(os.Stderr, "TYPESAFE_API_KEY is required")
			return 1
		}
		cwd, err := os.MkdirTemp("", "specaudit-codex-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(cwd)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		jevClient.Concurrency = cfg.Jev.Concurrency
		runner := audit.Runner{Codex: codex.Client{Command: cfg.LLM.BridgeCommand, Root: cfg.Root, CWD: cwd, Model: cfg.LLM.Model, Effort: cfg.LLM.Effort, Timeout: 3 * time.Minute}, Jev: jevClient}
		dir, err := runner.Run(ctx, cfg)
		if dir != "" {
			fmt.Fprintln(os.Stdout, filepath.Join(dir, "report.md"))
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			if errors.Is(err, audit.ErrPartial) {
				if errors.Is(ctx.Err(), context.Canceled) {
					return 130
				}
				return 2
			}
			return 1
		}
		return 0
	case "replay":
		flags := flag.NewFlagSet("replay", flag.ContinueOnError)
		dir := flags.String("run", "", "saved run directory")
		if err := flags.Parse(args[1:]); err != nil {
			return 1
		}
		if *dir == "" {
			fmt.Fprintln(os.Stderr, "--run is required")
			return 1
		}
		data, err := os.ReadFile(filepath.Join(*dir, "result.json"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		var result model.Result
		if err := json.Unmarshal(data, &result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		path := filepath.Join(*dir, "report.md")
		if err := report.Write(path, result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintln(os.Stdout, path)
		if result.Manifest.Status != "complete" {
			return 2
		}
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", args[0])
		return 1
	}
}
