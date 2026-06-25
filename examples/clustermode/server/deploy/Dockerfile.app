FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY . .
RUN cd examples/clustermode && go build -o server ./server

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/examples/clustermode/server .
EXPOSE 9001
ENTRYPOINT ["./server"]
