#!/usr/bin/env bash
set -euo pipefail

# -----------------------------
# Step 1: Delete and recreate the kind cluster
# -----------------------------
kind delete cluster --name kind || true
kind create cluster --config kind-config.yaml

# Wait for nodes to come up
echo "⏳ Waiting for nodes to be ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=180s

# -----------------------------
# Step 2: Copy credential provider plugin into control-plane node
# -----------------------------
docker cp ./credential-provider-plugin kind-control-plane:/etc/kubernetes/credential-providers

# -----------------------------
# Step 3: Update kubeadm-flags.env with credential provider config
# -----------------------------
docker cp kind-control-plane:/var/lib/kubelet/kubeadm-flags.env ./kubeadm-flags-kind.env

# Append flags if not already present
if ! grep -q "image-credential-provider" ./kubeadm-flags-kind.env; then
  echo -n " --image-credential-provider-bin-dir='/etc/kubernetes/credential-providers' --image-credential-provider-config='/etc/kubernetes/credential-providers/config.yml'" >> ./kubeadm-flags-kind.env
fi

# Copy back the updated file
docker cp ./kubeadm-flags-kind.env kind-control-plane:/var/lib/kubelet/kubeadm-flags.env

# -----------------------------
# Step 4: Restart kubelet inside control-plane node
# -----------------------------
docker exec -it kind-control-plane bash -c "systemctl daemon-reload && systemctl restart kubelet"

# Wait for nodes to be ready again
kubectl wait --for=condition=Ready nodes --all --timeout=180s

# -----------------------------
# Step 5: Apply RBAC for node credential providers
# -----------------------------
kubectl apply -f node-credential-providers.yaml

# -----------------------------
# Step 6: Verify node permissions (example checks)
# -----------------------------
kubectl auth can-i get serviceaccounts --as=system:node:kind-control-plane -n default
# kubectl auth can-i create serviceaccounts --as=system:node:kind-control-plane -n default || true

# -----------------------------
# Step 7: Deploy test pod
# -----------------------------
kubectl apply -f sapod-test.yaml

# Wait for pod to be running
echo "⏳ Waiting for test pod to be ready..."
kubectl wait --for=condition=Ready pod/busyboxes -n default --timeout=120s

# Show pod status
kubectl get pods -n default -o wide
