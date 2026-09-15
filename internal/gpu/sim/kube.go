// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package sim

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gputop/gputop/internal/kube"
)

var _ kube.Simulator = (*Provider)(nil)

const simNode = "gpu-node-01" // matches collector.simNode

func simPodUID(pid int) string { return fmt.Sprintf("5e1a7ed0-0000-4000-8000-%012x", pid) }

// SimulatedPods implements kube.Simulator: one pod per simulated GPU
// process plus a pod stuck in Pending for lack of GPUs.
func (p *Provider) SimulatedPods() []kube.PodInfo {
	t := p.elapsed()
	now := p.now()
	seen := map[string]bool{}
	var pods []kube.PodInfo
	for i := 0; i < p.opts.GPUs; i++ {
		for _, sp := range p.procs(i, t) {
			if seen[sp.pod] {
				continue
			}
			seen[sp.pod] = true
			created := p.start.Add(-3*time.Hour - time.Duration(i)*time.Minute)
			image := map[string]string{
				"trainer": "nvcr.io/nvidia/pytorch:24.08-py3", "vllm": "vllm/vllm-openai:v0.6.1",
				"triton": "nvcr.io/nvidia/tritonserver:24.08-py3", "notebook": "quay.io/jupyter/pytorch-notebook:cuda12-2024-09",
			}[sp.container]
			gpuRes := "1"
			if sp.part >= 0 {
				gpuRes = ""
			}
			c := kube.ContainerInfo{
				Name: sp.container, Image: image, ID: fmt.Sprintf("%064x", sp.pid), Ready: true, State: "running",
				StartedAt: created.Add(40 * time.Second),
				Requests:  map[string]string{"cpu": "8", "memory": "64Gi"},
				Limits:    map[string]string{"cpu": "16", "memory": "96Gi"},
			}
			if gpuRes != "" {
				c.Limits["nvidia.com/gpu"], c.Requests["nvidia.com/gpu"] = gpuRes, gpuRes
			} else {
				c.Limits["nvidia.com/mig-3g.40gb"], c.Requests["nvidia.com/mig-3g.40gb"] = "1", "1"
			}
			if p.role[i] == roleStraggler {
				c.Restarts, c.LastState, c.LastFinished = 2, "OOMKilled (exit 137)", now.Add(-47*time.Minute)
				c.StartedAt = now.Add(-46 * time.Minute)
			}
			owner, ownerName := sp.kind, sp.wl
			if sp.kind == "Deployment" {
				owner, ownerName = "ReplicaSet", sp.pod[:strings.LastIndex(sp.pod, "-")]
			}
			pods = append(pods, kube.PodInfo{
				UID: simPodUID(sp.pid), Name: sp.pod, Namespace: sp.ns, Node: simNode,
				PodIP: fmt.Sprintf("10.244.3.%d", 10+len(pods)), HostIP: "10.0.12.7", Phase: "Running",
				QoS: "burstable", Created: created, StartTime: created.Add(2 * time.Second),
				Labels: map[string]string{
					"app.kubernetes.io/name": sp.wl, "app.kubernetes.io/component": sp.container,
					"kueue.x-k8s.io/queue-name": sp.ns + "-queue",
				},
				OwnerKind: owner, OwnerName: ownerName, WorkloadKind: sp.kind, WorkloadName: sp.wl,
				GPURequests: 1, Containers: []kube.ContainerInfo{c},
				Conditions: []kube.PodCondition{
					{Type: "PodScheduled", Status: "True", LastTransition: created},
					{Type: "Initialized", Status: "True", LastTransition: created.Add(time.Second)},
					{Type: "ContainersReady", Status: "True", LastTransition: c.StartedAt},
					{Type: "Ready", Status: "True", LastTransition: c.StartedAt},
				},
				Source: "api",
			})
		}
	}
	pending := now.Add(-12 * time.Minute)
	pods = append(pods, kube.PodInfo{
		UID: simPodUID(999999), Name: "llama-70b-eval-0", Namespace: "ml-training", Phase: "Pending",
		QoS: "guaranteed", Created: pending,
		Labels:    map[string]string{"app.kubernetes.io/name": "llama-70b-eval"},
		OwnerKind: "Job", OwnerName: "llama-70b-eval", WorkloadKind: "Job", WorkloadName: "llama-70b-eval",
		GPURequests: 4,
		Containers: []kube.ContainerInfo{{
			Name: "eval", Image: "nvcr.io/nvidia/pytorch:24.08-py3",
			Requests: map[string]string{"cpu": "32", "memory": "256Gi", "nvidia.com/gpu": "4"},
			Limits:   map[string]string{"cpu": "32", "memory": "256Gi", "nvidia.com/gpu": "4"},
		}},
		Conditions: []kube.PodCondition{{Type: "PodScheduled", Status: "False", Reason: "Unschedulable",
			Message: "0/1 nodes are available: 1 Insufficient nvidia.com/gpu.", LastTransition: pending}},
		Source: "api",
	})
	kube.SortPods(pods)
	return pods
}

// SimulatedLogs implements kube.Simulator.
func (p *Provider) SimulatedLogs(pod kube.PodRef, container string, tail int) []string {
	now := p.now()
	t := p.elapsed()
	var lines []string
	stamp := func(back float64) string {
		return now.Add(-time.Duration(back * float64(time.Second))).UTC().Format("2006-01-02T15:04:05.000Z")
	}
	switch {
	case strings.HasPrefix(pod.Name, "llama-70b-pretrain"):
		rank := strings.TrimPrefix(pod.Name, "llama-70b-pretrain-worker-")
		step := int(t/6) + 41200
		lines = append(lines,
			stamp(3*3600)+" INFO  torch.distributed: initialized process group backend=nccl rank="+rank+" world_size=8",
			stamp(3*3600-5)+" INFO  NCCL INFO Using network IB · 8 GPUs · NVLink P2P enabled",
			stamp(3*3600-9)+" INFO  loading checkpoint s3://ml-ckpt/llama-70b/step-41000")
		for i := 40; i >= 0; i-- {
			s := step - i
			loss := 1.82 - 0.00002*float64(s-41000) + 0.01*math.Sin(float64(s))
			lines = append(lines, fmt.Sprintf("%s INFO  step %d | loss %.4f | lr 1.20e-04 | %.0f tok/s | mem 66.2GiB",
				stamp(float64(i)*6), s, loss, 3150+40*math.Sin(float64(s)/3)))
			if s%50 == 0 {
				lines = append(lines, stamp(float64(i)*6)+" INFO  saving checkpoint step-"+fmt.Sprint(s))
			}
		}
		if rank == "3" {
			lines = append(lines, stamp(1)+" WARN  rank 3 step time 2.1x median (PCIe link x8?)")
		}
	case strings.HasPrefix(pod.Name, "chat-api"):
		for i := 30; i >= 0; i-- {
			lines = append(lines, fmt.Sprintf("%s INFO:     10.244.1.%d - \"POST /v1/chat/completions HTTP/1.1\" 200 OK (%d ms)", stamp(float64(i)*2), 20+i%7, 180+i*13%400))
		}
	case strings.HasPrefix(pod.Name, "embed-svc"):
		for i := 20; i >= 0; i-- {
			lines = append(lines, fmt.Sprintf("%s I0915 triton] successfully loaded 'bge-large' version 1 · batch %d · queue %dms", stamp(float64(i)*3), 8+i%24, i%5))
		}
	case strings.HasPrefix(pod.Name, "notebook"):
		lines = append(lines, stamp(9000)+" [I ServerApp] Jupyter Server 2.14 is running at http://notebook-alice-0:8888/lab",
			stamp(2400)+" [I KernelManager] Kernel started: 3f6c…",
			stamp(300)+" [I ServerApp] Saving file at /work/finetune.ipynb")
	default:
		return nil
	}
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	return lines
}

// SimulatedEvents implements kube.Simulator.
func (p *Provider) SimulatedEvents(pod kube.PodRef) []kube.PodEvent {
	now := p.now()
	if pod.Name == "llama-70b-eval-0" {
		return []kube.PodEvent{{Type: "Warning", Reason: "FailedScheduling", From: "default-scheduler", Count: 14,
			Message: "0/1 nodes are available: 1 Insufficient nvidia.com/gpu. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod.",
			First:   now.Add(-12 * time.Minute), Last: now.Add(-40 * time.Second)}}
	}
	start := p.start.Add(-3 * time.Hour)
	evs := []kube.PodEvent{
		{Type: "Normal", Reason: "Scheduled", From: "default-scheduler", Count: 1, Message: "Successfully assigned " + pod.Namespace + "/" + pod.Name + " to " + simNode, First: start, Last: start},
		{Type: "Normal", Reason: "Pulled", From: "kubelet", Count: 1, Message: "Container image already present on machine", First: start.Add(3 * time.Second), Last: start.Add(3 * time.Second)},
		{Type: "Normal", Reason: "Started", From: "kubelet", Count: 1, Message: "Started container", First: start.Add(4 * time.Second), Last: start.Add(4 * time.Second)},
	}
	if pod.Name == "llama-70b-pretrain-worker-3" {
		evs = append(evs,
			kube.PodEvent{Type: "Warning", Reason: "OOMKilling", From: "kernel-monitor", Count: 2, Message: "Memory cgroup out of memory: Killed process 210403 (python)", First: now.Add(-95 * time.Minute), Last: now.Add(-47 * time.Minute)},
			kube.PodEvent{Type: "Warning", Reason: "BackOff", From: "kubelet", Count: 3, Message: "Back-off restarting failed container trainer", First: now.Add(-94 * time.Minute), Last: now.Add(-46 * time.Minute)})
	}
	return evs
}
