FROM golang:1.23-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o arcsync ./cmd/server

# ---- Final image ----
FROM scratch
COPY --from=builder /build/arcsync /arcsync
EXPOSE 8080
ENTRYPOINT ["/arcsync"]
