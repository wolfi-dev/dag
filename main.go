package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/wolfi-dev/dag/pkg/commands"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := commands.Root.ExecuteContext(ctx); err != nil {
		log.Fatal("error during command execution:", err)
	}
}
