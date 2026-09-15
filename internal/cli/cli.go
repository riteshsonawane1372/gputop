// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package cli implements the gputop command line.
package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gputop/gputop/internal/app"
	"github.com/gputop/gputop/internal/buildinfo"
	"github.com/gputop/gputop/internal/config"
	"github.com/gputop/gputop/internal/keymap"
	"github.com/gputop/gputop/internal/logging"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/paths"
	"github.com/gputop/gputop/internal/remote"
	"github.com/gputop/gputop/internal/server"
	"github.com/gputop/gputop/internal/theme"
	"github.com/gputop/gputop/internal/tui"
	"github.com/mattn/go-isatty"
)

type flags struct {
	config      string
	version     bool
	printConfig bool
	once        bool
	json        bool
	service     bool
	listen      string
	remote      string
	demo        bool
	demoGPUs    int
	noHistory   bool
	interval    time.Duration
	retention   time.Duration
	theme       string
	debug       bool
	logFile     string
	genToken    bool
	listThemes  bool
}

const usage = `gputop — GPU monitoring for the terminal

Usage:
  gputop [flags]

Modes:
  gputop                      interactive TUI (default)
  gputop --once               print a one-shot summary and exit
  gputop --once --json        print one JSON snapshot and exit
  gputop --json               stream JSON snapshots (one per line)
  gputop --service            run headless: collect, keep history, serve API + /metrics
  gputop --remote NODE        open the TUI for a remote gputop agent
  gputop --demo               explore the UI with simulated GPUs

Flags:
`

// Main runs the CLI and returns the process exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	var f flags
	fs := flag.NewFlagSet("gputop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.config, "config", "", "config file (default "+paths.ConfigFile()+")")
	fs.StringVar(&f.config, "c", "", "shorthand for --config")
	fs.BoolVar(&f.version, "version", false, "print version and exit")
	fs.BoolVar(&f.version, "v", false, "shorthand for --version")
	fs.BoolVar(&f.printConfig, "print-config", false, "print the effective configuration and exit")
	fs.BoolVar(&f.once, "once", false, "collect once, print and exit")
	fs.BoolVar(&f.json, "json", false, "machine-readable JSON output")
	fs.BoolVar(&f.service, "service", false, "run as a headless agent serving the HTTP API")
	fs.StringVar(&f.listen, "listen", "", "service listen address (overrides server.listen)")
	fs.StringVar(&f.remote, "remote", "", "connect to a remote agent (configured node name or https URL)")
	fs.BoolVar(&f.demo, "demo", false, "use simulated GPUs (no hardware is read)")
	fs.IntVar(&f.demoGPUs, "demo-gpus", 8, "number of simulated GPUs with --demo")
	fs.BoolVar(&f.noHistory, "no-history", false, "disable the history store")
	fs.DurationVar(&f.interval, "interval", 0, "refresh interval (overrides refresh.interval)")
	fs.DurationVar(&f.retention, "retention", 0, "history retention (overrides history.retention)")
	fs.StringVar(&f.theme, "theme", "", "color theme (overrides theme.name)")
	fs.BoolVar(&f.listThemes, "list-themes", false, "list available themes and exit")
	fs.BoolVar(&f.debug, "debug", false, "enable debug logging (to the log file in TUI mode)")
	fs.StringVar(&f.logFile, "log-file", "", "write logs to this file")
	fs.BoolVar(&f.genToken, "gen-token", false, "generate an API token and its SHA-256 digest, then exit")
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nDocumentation: https://github.com/gputop/gputop\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "gputop: unexpected argument %q (see gputop --help)\n", fs.Arg(0))
		return 2
	}

	if f.version {
		fmt.Fprintln(stdout, buildinfo.String())
		return 0
	}
	if f.genToken {
		return genToken(stdout, stderr)
	}

	cfg, err := loadConfig(&f)
	if err != nil {
		fmt.Fprintf(stderr, "gputop: %v\n", err)
		return 1
	}
	if f.printConfig {
		b, err := cfg.Marshal()
		if err != nil {
			fmt.Fprintf(stderr, "gputop: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(b)
		return 0
	}
	if f.listThemes {
		for _, n := range theme.Builtin() {
			fmt.Fprintln(stdout, n)
		}
		if entries, err := os.ReadDir(paths.ThemesDir()); err == nil {
			for _, e := range entries {
				if ext := filepath.Ext(e.Name()); ext == ".yaml" || ext == ".yml" {
					fmt.Fprintf(stdout, "%s (user)\n", strings.TrimSuffix(e.Name(), ext))
				}
			}
		}
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tuiMode := !f.once && !f.json && !f.service
	log, closeLog, err := setupLogging(cfg, &f, tuiMode)
	if err != nil {
		fmt.Fprintf(stderr, "gputop: logging: %v\n", err)
		return 1
	}
	defer closeLog()

	if f.remote != "" {
		return runRemote(ctx, cfg, &f, stdout, stderr)
	}
	switch {
	case f.service:
		return runService(ctx, cfg, &f, log, stderr)
	case f.once || f.json:
		return runHeadless(ctx, cfg, &f, log, stdout, stderr)
	}
	return runTUI(ctx, cfg, &f, log, stderr)
}

func loadConfig(f *flags) (config.Config, error) {
	cfg, err := config.Load(f.config, f.config != "")
	if err != nil {
		return cfg, err
	}
	if f.interval > 0 {
		cfg.Refresh.Interval = config.Duration(f.interval)
		if cfg.Refresh.Normal.D() < f.interval {
			cfg.Refresh.Normal = config.Duration(f.interval)
		}
	}
	if f.retention > 0 {
		cfg.History.Retention = config.Duration(f.retention)
	}
	if f.noHistory {
		cfg.History.Enabled = false
	}
	if f.theme != "" {
		cfg.Theme.Name = f.theme
	}
	if f.listen != "" {
		cfg.Server.Listen = f.listen
	}
	if f.debug {
		cfg.Logging.Level = "debug"
	}
	if f.logFile != "" {
		cfg.Logging.File = f.logFile
	}
	return cfg, cfg.Validate()
}

func setupLogging(cfg config.Config, f *flags, tuiMode bool) (*slog.Logger, func(), error) {
	o := logging.Options{Level: cfg.Logging.Level, File: paths.Expand(cfg.Logging.File)}
	if tuiMode {
		// Never write logs to the terminal the TUI is drawing on.
		if o.File == "" && f.debug {
			o.File = filepath.Join(paths.StateDir(), "gputop.log")
		}
	} else {
		o.Stderr = true
		o.JSON = f.service && !isatty.IsTerminal(os.Stderr.Fd())
	}
	return logging.New(o)
}

func genToken(stdout, stderr io.Writer) int {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		fmt.Fprintf(stderr, "gputop: %v\n", err)
		return 1
	}
	tok := hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(tok))
	fmt.Fprintf(stdout, "token:        %s\ntoken_sha256: %s\n\n", tok, hex.EncodeToString(sum[:]))
	fmt.Fprintln(stdout, "Agent config:  server.token_sha256: <token_sha256>   (the plaintext token is not needed on the agent)")
	fmt.Fprintln(stdout, "Client:        store the token in a 0600 file and set remote.nodes[].token_file, or export GPUTOP_TOKEN")
	return 0
}

func buildRuntime(cfg config.Config, f *flags, log *slog.Logger, noPersist bool) (*app.Runtime, error) {
	return app.Build(app.Options{Config: cfg, Demo: f.demo, DemoGPUs: f.demoGPUs, Log: log, NoPersist: noPersist})
}

func buildTheme(cfg config.Config) (*theme.Theme, []string) {
	var notices []string
	pal, err := theme.Resolve(cfg.Theme.Name, paths.ThemesDir(), cfg.Theme.Colors)
	name := cfg.Theme.Name
	if err != nil {
		notices = append(notices, "theme: "+err.Error())
		if pal.Primary == "" {
			pal, _ = theme.Resolve("green", "", nil)
			name = "green"
		}
	}
	return theme.New(name, pal, cfg.Theme.Transparent), notices
}

func buildKeys(cfg config.Config) (*keymap.Map, []string) {
	over := map[string][]string{}
	for k, v := range cfg.Keys {
		over[k] = v
	}
	keys, err := keymap.New(over)
	if err != nil {
		return keys, []string{strings.ReplaceAll(err.Error(), "\n  - ", "; ")}
	}
	return keys, nil
}

func requireTerminal(stderr io.Writer) bool {
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		fmt.Fprintln(stderr, "gputop: stdout is not a terminal; use --once, --json or --service for non-interactive use")
		return false
	}
	return true
}

func runTUI(ctx context.Context, cfg config.Config, f *flags, log *slog.Logger, stderr io.Writer) int {
	if !requireTerminal(stderr) {
		return 1
	}
	rt, err := buildRuntime(cfg, f, log, false)
	if err != nil {
		fmt.Fprintf(stderr, "gputop: %v\n", err)
		return 1
	}
	defer rt.Close()
	th, notices := buildTheme(cfg)
	keys, keyNotices := buildKeys(cfg)
	notices = append(notices, keyNotices...)
	if s := rt.History; s != nil {
		if st := s.Status(); st.Error != "" {
			notices = append(notices, "history: "+st.Error+" (keeping history in memory)")
		}
	}

	engineCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { _ = rt.Engine.Run(engineCtx); close(done) }()

	var nodes tui.NodeLister
	if len(cfg.Remote.Nodes) > 0 {
		fleet := remote.NewFleet(cfg.Remote)
		go fleet.Run(engineCtx)
		nodes = fleet
	}
	err = tui.Run(ctx, tui.Options{
		Source: rt.Engine, Theme: th, Keys: keys, DefaultTab: cfg.UI.DefaultTab,
		Fahrenheit: strings.EqualFold(cfg.UI.TemperatureUnit, "f"), ShowCmd: cfg.UI.ShowCommandLines,
		Refresh: cfg.Refresh.Interval.D(), Nodes: nodes, Notices: notices,
	})
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		log.Warn("collectors did not stop within 3s")
	}
	if err != nil {
		fmt.Fprintf(stderr, "gputop: %v\n", err)
		return 1
	}
	return 0
}

func runRemote(ctx context.Context, cfg config.Config, f *flags, stdout, stderr io.Writer) int {
	node, err := remote.Resolve(cfg.Remote, f.remote)
	if err != nil {
		fmt.Fprintf(stderr, "gputop: %v\n", err)
		return 1
	}
	client, err := remote.New(node, cfg.Refresh.Interval.D())
	if err != nil {
		fmt.Fprintf(stderr, "gputop: %v\n", err)
		return 1
	}
	if f.once || f.json {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		sub, unsub := client.Subscribe()
		defer unsub()
		go client.Run(cctx)
		for {
			select {
			case s := <-sub:
				if err := printSnapshot(stdout, s, f.json, !f.once); err != nil {
					fmt.Fprintf(stderr, "gputop: %v\n", err)
					return 1
				}
				if f.once {
					return 0
				}
			case <-cctx.Done():
				if f.once || ctx.Err() == nil {
					fmt.Fprintf(stderr, "gputop: remote %s: %v\n", node.Name, errOr(client.Err(), "timed out"))
					return 1
				}
				return 0
			}
		}
	}
	if !requireTerminal(stderr) {
		return 1
	}
	th, notices := buildTheme(cfg)
	keys, keyNotices := buildKeys(cfg)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go client.Run(cctx)
	err = tui.Run(ctx, tui.Options{
		Source: client, Theme: th, Keys: keys, DefaultTab: cfg.UI.DefaultTab,
		Fahrenheit: strings.EqualFold(cfg.UI.TemperatureUnit, "f"), Refresh: cfg.Refresh.Interval.D(),
		Remote: node.Name, Notices: append(notices, keyNotices...),
	})
	if err != nil {
		fmt.Fprintf(stderr, "gputop: %v\n", err)
		return 1
	}
	return 0
}

func errOr(err error, def string) string {
	if err != nil {
		return err.Error()
	}
	return def
}

func runService(ctx context.Context, cfg config.Config, f *flags, log *slog.Logger, stderr io.Writer) int {
	rt, err := buildRuntime(cfg, f, log, false)
	if err != nil {
		fmt.Fprintf(stderr, "gputop: %v\n", err)
		return 1
	}
	defer rt.Close()
	version, _, _ := buildinfo.Info()
	srv, err := server.New(server.Options{Server: cfg.Server, Prometheus: cfg.Prometheus, Source: rt.Engine, Version: version, Log: log})
	if err != nil {
		fmt.Fprintf(stderr, "gputop: %v\n", err)
		return 1
	}
	errCh := make(chan error, 2)
	go func() { errCh <- rt.Engine.Run(ctx) }()
	go func() { errCh <- srv.Run(ctx) }()
	if err := <-errCh; err != nil {
		fmt.Fprintf(stderr, "gputop: %v\n", err)
		return 1
	}
	return 0
}

func runHeadless(ctx context.Context, cfg config.Config, f *flags, log *slog.Logger, stdout, stderr io.Writer) int {
	if f.once {
		// One-shot mode reads the persisted history only through the event
		// log; it never writes history of its own.
		cfg.History.Enabled = false
		rt, err := buildRuntime(cfg, f, log, true)
		if err != nil {
			fmt.Fprintf(stderr, "gputop: %v\n", err)
			return 1
		}
		defer rt.Close()
		snap := rt.Engine.Once(ctx, 500*time.Millisecond)
		if err := printSnapshot(stdout, snap, f.json, false); err != nil {
			fmt.Fprintf(stderr, "gputop: %v\n", err)
			return 1
		}
		if len(snap.GPUs) == 0 && !f.json {
			return 3
		}
		return 0
	}
	rt, err := buildRuntime(cfg, f, log, false)
	if err != nil {
		fmt.Fprintf(stderr, "gputop: %v\n", err)
		return 1
	}
	defer rt.Close()
	sub, unsub := rt.Engine.Subscribe()
	defer unsub()
	go func() { _ = rt.Engine.Run(ctx) }()
	for {
		select {
		case <-ctx.Done():
			return 0
		case s := <-sub:
			if !s.Ready {
				continue
			}
			if err := printSnapshot(stdout, s, true, true); err != nil {
				return 1 // stdout closed (e.g. piped into head)
			}
		}
	}
}

func printSnapshot(w io.Writer, s *model.Snapshot, asJSON, stream bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		if !stream {
			enc.SetIndent("", "  ")
		}
		return enc.Encode(s)
	}
	color := false
	if file, ok := w.(*os.File); ok {
		color = isatty.IsTerminal(file.Fd())
	}
	return writeSummary(w, s, color)
}
