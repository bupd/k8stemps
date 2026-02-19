# Start from the kind node image (get the version that matches your kind version)
FROM kindest/node:v1.30.0

# Copy your credential provider plugin binary into the image
COPY ./credential-provider-plugin /etc/kubernetes/plugins/credential-providers

LABEL maintainer="Prasanth Baskar <bupdprasanth@gmail.com>"
# LABEL version="1.0"
# LABEL description="A simple project to serve a static page with Nginx in Docker."

# COPY serve/index.html /usr/share/nginx/html/
#
# EXPOSE 80
