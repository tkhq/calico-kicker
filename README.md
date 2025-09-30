# Calico Kicker

There are a number of race conditions in Calico's CNI calico-node combo container, particularly when dealing with more interesting networking systems.

The calico kicker is a crude tool which provides a certain amount of time for `calico-node` to get its configuration in order and then, if it fails to do so, kills the Pod, such that it can try again.

In order to operate, the `POD_NAME` environment variable _must_ be set, and the ServiceAccount under which the kicker runs must have delete privilege on Pods in the namespace in which it runs.
