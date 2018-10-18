## How to deploy read/write core pairs on Kubernetes

The current technique installs a separate rw-core deployment to each Kubernetes node, where each deployment consists of a pair (replicas = 2) of co-located rw-cores. Co-location is enforced by making use of the Kubernetes nodeSelector constraint applied at the pod spec level.

In order for node selection to work, a label must be applied to each node. There is a set of built-in node labels that comes with Kubernetes out of the box, one of which is kubernetes.io/hostname. This label can be used to constrain the deployment of a core pair to a node with a specific hostname. Another approach is to take greater control and create new node labels.

The following discussion assumes installation of the voltha-k8s-playground (https://github.com/ciena/voltha-k8s-playground) which configures three Kubernetes nodes named k8s1, k8s2, and k8s3.

Create a "nodename" label for each Kubernetes node:
```
kubectl label nodes k8s1 nodename=k8s1
kubectl label nodes k8s2 nodename=k8s2
kubectl label nodes k8s3 nodename=k8s3
```

Verify that the labels have been applied:
```
kubectl get nodes --show-labels
NAME      STATUS    ROLES         AGE       VERSION   LABELS
k8s1      Ready     master,node   4h        v1.9.5    ...,kubernetes.io/hostname=k8s1,nodename=k8s1
k8s2      Ready     node          4h        v1.9.5    ...,kubernetes.io/hostname=k8s2,nodename=k8s2
k8s3      Ready     node          4h        v1.9.5    ...,kubernetes.io/hostname=k8s3,nodename=k8s3
```


Ensure that a nodeSelector section appears in the deployment's pod spec (such a section should already exist in each manifest):
```
      ...
      nodeSelector:
        nodename: k8s1
```

Once the labels have been applied, deploy the 3 core pairs:
```
kubectl apply -f k8s/rw-core-pair1.yml
kubectl apply -f k8s/rw-core-pair2.yml
kubectl apply -f k8s/rw-core-pair3.yml
```
