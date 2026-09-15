# Kubernetes

> 🧪 Experimental. Kubernetes support is optional; gputop works on plain Linux.

## Detection

With `kubernetes.enabled: auto` (the default), gputop detects:

| Mode | How it is detected |
|---|---|
| `in-cluster` | `KUBERNETES_SERVICE_HOST` is set and a service account token is mounted |
| `node` | `/var/lib/kubelet/pods`, `/etc/kubernetes/kubelet.conf` or the pod log directory exists |
| `kubeconfig` | `$KUBECONFIG` or `~/.kube/config` exists (context shown; no API calls yet) |
| `none` | none of the above |

## Correlation chain

```text
NVML PID ─► /proc/<pid>/cgroup ─► container ID + pod UID + QoS
                                        │
            /var/log/pods/<ns>_<pod>_<uid>/ ─► namespace + pod name
                                        │
            API server (in-cluster) ─► container name + controller
                                        │
                     ReplicaSet ─► Deployment   (pod-template-hash contract)
                     Job        ─► CronJob      (Job ownerReferences)
```

1. **cgroups.** Parsing supports cgroup v1 and v2, the cgroupfs and systemd
   drivers, and the containerd, CRI-O, Docker and Podman naming schemes.
2. **Pod log directories.** Kubelet creates `/var/log/pods/<namespace>_<pod>_<uid>`.
   Namespaces and pod names cannot contain `_`, so the mapping is unambiguous.
3. **API server.** When running in a pod (or `kubernetes.api: true`), gputop
   lists pods on its node (`fieldSelector=spec.nodeName=<node>`) with the pod's
   service account. It resolves controllers:
   - ReplicaSet → Deployment by stripping the `pod-template-hash` label value,
     which is how the Deployment controller names ReplicaSets.
   - Job → CronJob by reading the Job's owner references (cached).
   - StatefulSet and DaemonSet are read directly from owner references.

Without API access, a Deployment may be inferred from a pod name of the form
`<deployment>-<hash>-<suffix>` using Kubernetes' generated-name alphabet. Such
results are marked as inferred (`workload_inferred: true`, shown with `?` or
`(inferred)` in the UI).

## PID namespaces

NVML reports host PIDs. gputop must see the host PID namespace to resolve them:
run the container with `hostPID: true` (or `docker run --pid=host`). If gputop
detects that it runs in a nested PID namespace, it shows process data from NVML
but does not look up `/proc`, so it never attributes GPU usage to an unrelated
process that happens to share a PID number.

## Deploying as a DaemonSet

`deploy/kubernetes/daemonset.yaml` contains a namespace-scoped service account,
a ClusterRole with `get`/`list` on pods and `get` on jobs, and a DaemonSet that:

- schedules on nodes labelled `nvidia.com/gpu.present=true`;
- uses `hostPID: true`, `NVIDIA_VISIBLE_DEVICES=all` and
  `NVIDIA_DRIVER_CAPABILITIES=utility` (NVML only);
- mounts `/var/log/pods` read-only;
- runs `gputop --service` on loopback.

Access the agent with `kubectl port-forward` or configure TLS and token
authentication ([remote.md](remote.md)). The manifest references a container
image; building and publishing an official image is on the roadmap. Until then,
package the release binary on a minimal glibc base image (for example
`gcr.io/distroless/base-debian12`).

## Required RBAC

```yaml
rules:
  - apiGroups: [""]
    resources: [pods]
    verbs: [get, list]
  - apiGroups: [batch]
    resources: [jobs]
    verbs: [get]        # optional: only to resolve CronJobs
```

If the API returns `403`, gputop keeps working with cgroup and pod log
correlation and shows the error in the Kubernetes tab.

## Limitations and plans

- GPU **allocation** is inferred from processes. A pod that has been assigned a
  GPU but has not yet created a CUDA context is not shown as allocated.
  Integration with the kubelet pod-resources API is planned.
- Kubeconfig-based API access (outside a pod) is planned.
- Cluster name is not reported (the API does not expose one uniformly).
