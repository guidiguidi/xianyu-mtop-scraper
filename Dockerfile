# syntax=docker/dockerfile:1

# ── Layer 1: Compile the Go Application ──────────────────────────────────────
FROM --platform=linux/amd64 golang:1.26 AS builder

WORKDIR /app

# Enable CGO for go-sqlite3 compilation
ENV CGO_ENABLED=1

# Copy dependency mappings
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build
COPY . .
RUN go build -ldflags="-s -w" -o goofish_backend main.go

# ── Layer 2: Prepare the Production Runner ────────────────────────────────────
FROM --platform=linux/amd64 python:3.11-slim

WORKDIR /app

# Set env parameters
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PLAYWRIGHT_BROWSERS_PATH=/app/pw-browsers

# Install base network tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl ca-certificates && \
    rm -rf /var/lib/apt/lists/*

# Install python dependencies for our browser refresher helper
COPY requirements.txt .
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install --upgrade pip && \
    pip install -r requirements.txt

# Install Playwright and Chromium dependencies
RUN python -m playwright install chromium && \
    python -m playwright install-deps chromium

# Copy compiled Go application from Builder stage
COPY --from=builder /app/goofish_backend ./goofish_backend

# Copy python script helpers and remaining resources
COPY . .

# Expose Gin REST API port
EXPOSE 8080

# Execute Go application natively
CMD ["./goofish_backend"]