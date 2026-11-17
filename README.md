# Calico Kicker

There are a number of race conditions in Calico's CNI calico-node combo container, particularly when dealing with more interesting networking systems.

The calico kicker watches for the WireGuard addresses on `wireguard.cali` and `wg-v6.cali`.  Calico's tunnel-IP allocator, which runs inside `calico-node`'s `node-services` daemon, can drop a WireGuard key update that arrives mid-reconcile, leaving a node with only one of the two addresses.  The kicker works in two stages:

1. After `-remediate-after` (default `1m`) without all addresses, on every check it restarts `node-services` by running `sv restart /etc/service/enabled/node-services` in the `calico-node` container of its own Pod (via `pods/exec`).  The fresh daemon allocates the missing address in its initial reconcile.  The kicker never runs a second, concurrent allocator, which could leak or release addresses.  A non-zero exit, or `sv` printing `timeout:` or `fail:`, counts as a failed attempt.  Each attempt has a 60s limit.
2. If the addresses are still missing after `-termination-timeout` (default `3m`), it deletes its own Pod (grace period 10s) so `calico-node` starts over.  Once the delete is accepted, it stops checking.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-check-interval` | `30s` | how often to check for addresses |
| `-remediate-after` | `1m` | when to start restarting `node-services`; must be less than `-termination-timeout` |
| `-remediate-command` | `sv restart /etc/service/enabled/node-services` | command (split on whitespace) run in the container |
| `-termination-timeout` | `3m` | when to give up and delete the Pod |
| `-calico-container` | `calico-node` | container in which to run the command |
| `-interface-names` | WireGuard defaults | comma-separated interfaces to monitor |

In order to operate, the `POD_NAME` environment variable _must_ be set, the ServiceAccount token must be automounted, and the ServiceAccount under which the kicker runs must be able to `delete` `pods` and `get` and `create` `pods/exec` in the namespace in which it runs (the Kustomize component grants these).  `get` covers the WebSocket exec, `create` the SPDY fallback.

The kicker fails fast: if `POD_NAME` or the in-cluster config is missing, or `-remediate-command` is empty, it exits 1 at startup.  A crash-looping sidecar makes the `calico-node` Pod show as unready; the Kustomize component always sets `POD_NAME`.

# Kustomize

This package comes with a Kustomize _Component_, found in the `kustomize/` directory.  Note that as a component, it must be included as such, not as a resource.

While this will work with a standard Calico deployment, if you do not run Calico in its own `calico` namespace, you must also set `POD_NAMESPACE`.  For instance, the _default_ namespace for upstream calico is `kube-system`.
