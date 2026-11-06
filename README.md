# Calico Kicker

There are a number of race conditions in Calico's CNI calico-node combo container, particularly when dealing with more interesting networking systems.

The calico kicker is a crude tool which provides a certain amount of time for `calico-node` to get its configuration in order and then, if it fails to do so, kills the Pod, such that it can try again.

In order to operate, the `POD_NAME` environment variable _must_ be set, and the ServiceAccount under which the kicker runs must have delete privilege on Pods in the namespace in which it runs.

## Readiness

Run with `-ready`, the kicker performs the same address check once and exits 0 if every monitored interface has its address, 1 otherwise. The Kustomize component wires this up as an `exec` readiness probe on the kicker container, so the `calico-node` Pod reports not-Ready until Calico's addresses actually exist. Anything that waits on Pod readiness — DaemonSet rollouts, node-roll tooling — therefore sees the race instead of a Pod that looks healthy right up until the kicker deletes it.

The `-interface-names` override must be passed to the probe command as well as the main process if you use it.

# Kustomize

This package comes with a Kustomize _Component_, found in the `kustomize/` directory.  Note that as a component, it must be included as such, not as a resource.

While this will work with a standard Calico deployment, if you do not run Calico in its own `calico` namespace, you must also set `POD_NAMESPACE`.  For instance, the _default_ namespace for upstream calico is `kube-system`.
