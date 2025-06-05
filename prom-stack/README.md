## follow this guide to setup prom-stack on any cluster
video: https://www.youtube.com/watch?v=fzny5uUaAeY
blog: https://technotim.live/posts/kube-grafana-prometheus/
repo/codes: https://github.com/techno-tim/launchpad/tree/master/kubernetes/kube-prometheus-stack


- First setup k8s or k3s cluster
- then create a namespace `monitoring`

## Prerequisite for k3s clusters
you need to spun up a cluster with these server args
with these arguments you will get more charts and info about the clusters
#### Note: I will deal with you later

```
extra_server_args: "--no-deploy servicelb --no-deploy traefik --kube-controller-manager-arg bind-address=0.0.0.0 --kube-proxy-arg metrics-bind-address=0.0.0.0 --kube-scheduler-arg bind-address=0.0.0.0 --etcd-expose-metrics true --kubelet-arg containerd=/run/k3s/containerd/containerd.sock"
```

execute: 
## to create a secret

```sh
kubectl create secret generic grafana-admin-credentials --from-file=./admin-user --from-file=admin-password -n monitoring
``` 

## verify the secret
```sh
kubectl describe secret -n monitoring grafana-admin-credentials
```

## Install Chart 
For normal clusters
```sh 
helm install -n monitoring prometheus prometheus-community/kube-prometheus-stack -f values.yaml
```

For K3s clusters
```sh 
helm install -n monitoring prometheus prometheus-community/kube-prometheus-stack -f k3s-values.yaml
```

## Port Forward grafana UI in the freelens 
```
kubectl port-forward -n monitoring grafana-fcc55c57f-fhjfr 52222:3000
```
