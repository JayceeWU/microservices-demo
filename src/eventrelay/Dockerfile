FROM python:3.13-alpine
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY relay.py saga.py health_server.py ./
COPY gen ./gen
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PYTHONPATH=/app/gen
USER 65532:65532
ENTRYPOINT ["python"]
