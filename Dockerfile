FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go mod tidy && go build -o main cmd/api/main.go

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/main .
COPY web/ ./web/
RUN mkdir -p data/uploads
EXPOSE 8080
CMD ["./main"]
