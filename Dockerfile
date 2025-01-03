FROM nginx:alpine

LABEL maintainer="Prasanth Baskar <bupdprasanth@gmail.com>"
LABEL version="1.0"
LABEL description="A simple project to serve a static page with Nginx in Docker."

COPY serve/index.html /usr/share/nginx/html/

EXPOSE 80
