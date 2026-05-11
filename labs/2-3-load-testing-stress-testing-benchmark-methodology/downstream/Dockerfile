FROM golang:1.24-alpine AS builder
WORKDIR /src

COPY downstream/go.mod ./
RUN go mod download all || true

COPY downstream/ ./
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/downstream ./

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget
COPY --from=builder /out/downstream /usr/local/bin/downstream

EXPOSE 8081
USER nobody
ENTRYPOINT ["/usr/local/bin/downstream"]
