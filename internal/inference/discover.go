// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package inference

import (
	"net"
	"path"
	"slices"
	"strconv"
	"strings"
)

// Proc is a GPU process considered for discovery.
type Proc struct {
	PID     int
	Command string
	GPU     int
	// Containerized processes are reached through PodIP; without one
	// they are skipped (their ports are not on the host's loopback).
	Containerized bool
	PodIP         string
	Pod           string
}

// launcher describes how to recognize a server and find its metrics port.
type launcher struct {
	engine string
	match  func(args []string) bool
	// portFlag is the flag carrying the metrics port and port its default.
	portFlag string
	port     int
}

func hasArg(args []string, want ...string) bool {
	for _, a := range args {
		if slices.Contains(want, a) || slices.Contains(want, path.Base(a)) {
			return true
		}
	}
	return false
}

func hasSub(args []string, bin, sub string) bool {
	for i, a := range args {
		if path.Base(a) == bin && i+1 < len(args) && args[i+1] == sub {
			return true
		}
	}
	return false
}

var launchers = []launcher{
	{engine: EngineVLLM, portFlag: "--port", port: 8000, match: func(a []string) bool {
		return hasSub(a, "vllm", "serve") || hasArg(a, "vllm.entrypoints.openai.api_server", "vllm.entrypoints.api_server")
	}},
	// SGLang serves /metrics only with --enable-metrics.
	{engine: EngineSGLang, portFlag: "--port", port: 30000, match: func(a []string) bool {
		return hasArg(a, "sglang.launch_server") || hasSub(a, "sglang", "serve")
	}},
	// TGI serves Prometheus on a separate port.
	{engine: EngineTGI, portFlag: "--prometheus-port", port: 9000, match: func(a []string) bool {
		return hasArg(a, "text-generation-launcher", "text-generation-router")
	}},
	// llama-server serves /metrics only with --metrics.
	{engine: EngineLlamaCpp, portFlag: "--port", port: 8080, match: func(a []string) bool {
		return hasArg(a, "llama-server")
	}},
}

// flag returns the value of --name or --name=value.
func flag(args []string, name string) (string, bool) {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1], true
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v, true
		}
	}
	return "", false
}

// Discover recognizes inference servers among GPU processes and returns
// one target per metrics endpoint, merging processes that share one
// (e.g. tensor-parallel workers).
func Discover(procs []Proc) []Target {
	var out []Target
	byURL := map[string]int{}
	for _, p := range procs {
		args := strings.Fields(p.Command)
		if len(args) == 0 {
			continue
		}
		for _, l := range launchers {
			if !l.match(args) {
				continue
			}
			port := l.port
			if v, ok := flag(args, l.portFlag); ok {
				n, err := strconv.Atoi(v)
				if err != nil || n <= 0 || n > 65535 {
					break
				}
				port = n
			}
			host := "127.0.0.1"
			switch {
			case p.Containerized && p.PodIP == "":
				host = ""
			case p.Containerized:
				host = p.PodIP
			default:
				if h, ok := flag(args, "--host"); ok && h != "" && h != "0.0.0.0" && h != "::" && h != "[::]" {
					host = h
				}
			}
			if host == "" {
				break
			}
			url := "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/metrics"
			if i, ok := byURL[url]; ok {
				t := &out[i]
				if !slices.Contains(t.GPUs, p.GPU) {
					t.GPUs = append(t.GPUs, p.GPU)
					slices.Sort(t.GPUs)
				}
				t.PID = min(t.PID, p.PID)
				break
			}
			name := l.engine + ":" + strconv.Itoa(port)
			if p.Pod != "" {
				name = p.Pod
			}
			byURL[url] = len(out)
			out = append(out, Target{Name: name, URL: url, Origin: OriginDiscovered, PID: p.PID, GPUs: []int{p.GPU}, Pod: p.Pod})
			break
		}
	}
	return out
}
