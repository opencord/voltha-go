## Running Voltha 2 in a Single-node Kubernetes Environment

One way to do this is to set up the Vagrant environment for Voltha 1 by following the instructions
in the BUILD.md file from the Voltha 1 repository at https://github.com/opencord/voltha.

To build the Voltha 2 images, follow the instructions in voltha-go/python/adapters/README.md and
ensure those images are available from within the Vagrant environment.

Copy the appropriate Kubernetes manifests from the voltha-go/k8s directory.

### Deploying Voltha 2

To deploy Voltha 2 apply the following manifests (On a single node the affinity router is not required):
```
k8s/namespace.yml
k8s/single-node/zookeeper.yml
k8s/single-node/kafka.yml
k8s/operator/etcd/cluster-role.yml
k8s/operator/etcd/cluster-role-binding.yml
k8s/operator/etcd/operator.yml
k8s/single-node/etcd_cluster.yml
k8s/single-node/rw-core.yml
k8s/adapters-ponsim.yml
k8s/single-node/ofagent.yml
k8s/single-node/cli.yml
```

### Running PONsim

To deploy the pods required to create PONsim devices, run ONOS,  and test EAP authentication:
```
k8s/genie-cni-plugin-1.8.yml
k8s/single-node/freeradius-config.yml
k8s/single-node/freeradius.yml
k8s/onos.yml
k8s/single-node/olt.yml
k8s/single-node/onu.yml
k8s/rg.yml
```
