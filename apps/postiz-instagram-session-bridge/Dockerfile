FROM python:3.13-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    HOME=/tmp

WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir --requirement requirements.txt

COPY bridge.py server.py ./
COPY test_bridge.py test_server.py /tmp/tests/
RUN cd /tmp/tests && PYTHONPATH=/app python -m unittest -v && rm -rf /tmp/tests

USER 65532:65532

EXPOSE 8080
CMD ["python", "server.py"]
