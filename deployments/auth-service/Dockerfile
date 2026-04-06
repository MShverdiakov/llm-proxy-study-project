FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/auth-service ./cmd/auth-service/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/auth-service /bin/auth-service
EXPOSE 8001
CMD ["/bin/auth-service"]
