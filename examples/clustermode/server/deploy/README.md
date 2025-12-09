# Cluster Mode Deployment

This directory contains the Docker Compose configuration for deploying the A2A cluster mode server.

## Prerequisites

- Docker
- Docker Compose

## Configuration

1. Copy the example environment file to `.env`:
   ```bash
   cp env.example .env
   ```

2. Edit `.env` to set your desired passwords and configuration:
   ```ini
   MYSQL_ROOT_PASSWORD=your_secure_password
   MYSQL_DATABASE=a2a
   ```

## Running the Cluster

Start the cluster using Docker Compose:

```bash
docker compose up -d
```

This will start:
- 3 replicas of the [server](./examples/clustermode/server/main.go) application
- 1 MySQL instance
- 1 Nginx load balancer

## Accessing the Cluster

The application is accessible via the Nginx load balancer on port 8080.
