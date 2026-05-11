FROM golang:1.24-alpine AS builder
WORKDIR /src

COPY sut/go.mod ./
RUN go mod download all || true

COPY sut/ ./
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sut ./

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget
COPY --from=builder /out/sut /usr/local/bin/sut

EXPOSE 8080
USER nobody
ENTRYPOINT ["/usr/local/bin/sut"]
