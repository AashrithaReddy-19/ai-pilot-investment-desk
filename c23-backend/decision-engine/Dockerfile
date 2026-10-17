# Internal AI/analytics service (FastAPI). Not exposed publicly; only the Go API calls it.
#   docker build -t c23-ai decision-engine
FROM python:3.12-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY requirements-ai.txt .
RUN pip install --no-cache-dir -r requirements-ai.txt
COPY app ./app
RUN useradd --system --no-create-home appuser
USER appuser
EXPOSE 8001
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8001/health', timeout=2)"
CMD ["uvicorn", "app.ai_service:app", "--host", "0.0.0.0", "--port", "8001"]
