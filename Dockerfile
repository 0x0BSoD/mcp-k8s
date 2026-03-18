FROM golang:1.25 AS builder

ARG CMD
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/${CMD} ./cmd/${CMD}

# ──────────────────────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS runtime

ARG CMD
COPY --from=builder /out/${CMD} /app

USER nonroot:nonroot
ENTRYPOINT ["/app"]
