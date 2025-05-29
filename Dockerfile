# Stage 1: Builder
FROM golang:1.23-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod/ \
  --mount=type=bind,source=go.sum,target=go.sum \
  --mount=type=bind,source=go.mod,target=go.mod \
  go mod download -x

COPY . .

ENV GOCACHE=/root/.cache/go-build
RUN --mount=type=cache,target=/go/pkg/mod/ \
  --mount=type=cache,target="/root/.cache/go-build" \
  --mount=type=bind,target=. \
  CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /schedy ./cmd/schedy/main.go

# Stage 2: Runtime
FROM alpine:3.19
RUN mkdir /data && chown nobody:nobody /data
RUN apk --no-cache add ca-certificates
WORKDIR /
COPY --from=builder /schedy /schedy
EXPOSE 8080
USER nobody:nobody
CMD ["/schedy"]