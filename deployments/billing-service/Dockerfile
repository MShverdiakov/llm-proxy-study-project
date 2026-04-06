FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/billing-service ./cmd/billing-service/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/billing-service /bin/billing-service
EXPOSE 8002
CMD ["/bin/billing-service"]
