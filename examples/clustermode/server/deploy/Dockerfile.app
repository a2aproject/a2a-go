FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o server ./examples/clustermode/server

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/server .
EXPOSE 9001
ENTRYPOINT ["./server"]
