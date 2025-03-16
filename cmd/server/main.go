package main

import (
	"github.com/maks112v/minicast/pkg/server"
	"go.uber.org/zap"
)

func main() {
	zap, _ := zap.NewProduction()
	defer zap.Sync()
	logger := zap.Sugar().With("module", "server")

	srv := server.New(logger)
	logger.Fatal(srv.Start(":8001"))
}
