FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/llm-proxy ./cmd/llm-proxy/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/llm-proxy /bin/llm-proxy
EXPOSE 8000
CMD ["/bin/llm-proxy"]
