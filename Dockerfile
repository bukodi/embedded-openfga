FROM golang:1.24 AS builder
COPY . /app
WORKDIR /app
RUN go mod tidy && \
    go mod download && \
    CGO_ENABLED=1 go build -o ./embd-openfga cmd/*.go

FROM debian:bookworm-slim

COPY --from=builder /app/embd-openfga /app/embd-openfga
WORKDIR /app
COPY ./templates /app/templates
COPY ./model.fga /app/model.fga

RUN chmod +x /app/embd-openfga
ENTRYPOINT ["/app/embd-openfga"]
