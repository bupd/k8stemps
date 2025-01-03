# Use the official Nginx image as a base
FROM nginx:alpine

# Install bash and curl (you'll need bash to handle the shutdown logic)
RUN apk add --no-cache bash curl

# Copy the custom Nginx config (optional)
COPY nginx.conf /etc/nginx/nginx.conf

# Add a script that will monitor for a self-destroy request
COPY destroy.sh /usr/local/bin/destroy.sh
RUN chmod +x /usr/local/bin/destroy.sh

# Expose the default HTTP port
EXPOSE 80

# Start Nginx in the foreground, then watch for the shutdown trigger
CMD ["sh", "-c", "nginx && /usr/local/bin/destroy.sh"]
