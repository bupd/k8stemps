#!/bin/bash

# --- CHANGE: dynamically take inputs ---
read -p "Registry URL: " REGISTRY       # e.g. https://8gears.container-registry.com
read -p "Username: " USERNAME          # e.g. robot_8gcr+bupd_pull
read -p "Password: " PASSWORD          # e.g. TFLf3JVLc0JJ...
read -p "Secret name: " SECRET_NAME     # e.g. 8gears-cr-cred

# Build auth string
# --- CHANGE: auth must be base64(user:pass) ---
AUTH=$(echo -n "$USERNAME:$PASSWORD" | base64)

# Build the docker JSON config
# --- CHANGE: JSON dynamically constructed ---
DOCKER_CONFIG=$(cat <<EOF
{
  "auths": {
    "$REGISTRY": {
      "username": "$USERNAME",
      "password": "$PASSWORD",
      "auth": "$AUTH"
    }
  }
}
EOF
)

# Base64 encode entire JSON
ENCODED=$(echo -n "$DOCKER_CONFIG" | base64 -w 0)  # -w 0 removes line breaks

# --- CHANGE: print YAML output ---
echo ""
echo "Generated Kubernetes Secret:"
echo ""
cat <<EOF
apiVersion: v1
kind: Secret
type: kubernetes.io/dockerconfigjson
metadata:
  name: $SECRET_NAME
data:
  .dockerconfigjson: $ENCODED
EOF
