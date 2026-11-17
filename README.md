# Calico Kicker

There are a number of race conditions in Calico's CNI calico-node combo container, particularly when dealing with more interesting networking systems.

The calico kicker is a crude tool which watches for the WireGuard addresses on `wireguard.cali` and `wg-v6.cali`.  Calico's tunnel-IP allocator daemon can drop a WireGuard key update that arrives mid-reconcile, leaving a node with only one of the two addresses.  The kicker works in two stages:

1. After `-allocate-after` (default `1m`) without all addresses, on every check it runs `calico-node -allocate-tunnel-addrs -allocate-tunnel-addrs-run-once` inside the `calico-node` container of its own Pod (via `pods/exec`).  This is the same one-shot allocator `calico-node` runs at startup.
2. If the addresses are still missing after `-termination-timeout` (default `3m`), it deletes its own Pod (grace period 10s) so `calico-node` starts over.  Once the delete is accepted, it stops checking.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-check-interval` | `30s` | how often to check for addresses |
| `-allocate-after` | `1m` | when to start running the one-shot allocator; must be less than `-termination-timeout` |
| `-termination-timeout` | `3m` | when to give up and delete the Pod |
| `-calico-container` | `calico-node` | container in which to run the allocator |
| `-interface-names` | WireGuard defaults | comma-separated interfaces to monitor |

In order to operate, the `POD_NAME` environment variable _must_ be set, and the ServiceAccount under which the kicker runs must have `delete` on `pods` and `create` on `pods/exec` in the namespace in which it runs (the Kustomize component grants both).

# Kustomize

This package comes with a Kustomize _Component_, found in the `kustomize/` directory.  Note that as a component, it must be included as such, not as a resource.

While this will work with a standard Calico deployment, if you do not run Calico in its own `calico` namespace, you must also set `POD_NAMESPACE`.  For instance, the _default_ namespace for upstream calico is `kube-system`.
