# Go + Gin public API. Build context: repository root (so the immutable official data can be copied in).
#   docker build -f backend/Dockerfile -t c23-api .
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
# SHA-256 verified at import time against MANIFEST.json
COPY decision-engine/data/raw /data/raw
ENV RAW_DATA_DIR=/data/raw PORT=8080
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD ["/api", "-healthcheck"]
ENTRYPOINT ["/api"]
