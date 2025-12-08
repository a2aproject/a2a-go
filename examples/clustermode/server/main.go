// Copyright 2025 The A2A Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

var (
	port   = flag.Int("port", 9001, "Port for a gGRPC A2A server to listen on.")
	dbName = flag.String("db", "", "Database connection string (DSN).")
)

func main() {
	flag.Parse()

	agentCard := &a2a.AgentCard{
		Name:               "A2A Cluster",
		URL:                fmt.Sprintf("http://127.0.0.1:%d/invoke", *port),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to bind to a port: %v", err)
	}

	workerID := uuid.NewString()
	db := openDB()
	conf := &a2asrv.ClusterConfig{
		TaskStore:    newDBTaskStore(db),
		QueueManager: newDBEventQueueManager(db),
		WorkQueue:    newDBWorkQueue(db, workerID),
	}
	requestHandler := a2asrv.NewHandler(
		&agentExecutor{},
		a2asrv.WithClusterMode(conf),
	)
	mux := http.NewServeMux()
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(requestHandler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	log.Printf("%s joined cluster", workerID)

	err = http.Serve(listener, mux)
	log.Printf("Server stopped: %v", err)
}

func openDB() *sql.DB {
	if *dbName == "" {
		log.Fatal("Database connection string (DSN) is required. Please provide it using -db flag.")
	}
	db, err := sql.Open("mysql", *dbName)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	db.SetConnMaxLifetime(3 * time.Minute)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	return db
}
