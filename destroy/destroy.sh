#!/bin/bash

# Keep the container alive until it receives the /destroy request
while true; do
    # Wait for the HTTP request that triggers the shutdown (curl check or any other method)
    curl -s http://localhost/destroy || break

    # Trigger the self-destruction (exit to shut down the container)
    echo "Destroying container as per the request..."
    exit 0
done
