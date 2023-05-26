package main

import (
	"context"
	"github.com/cirruslabs/cirrus-backbone-services/pkg/commands"
	"log"
	"os"
	"os/signal"
)

func main() {
	if err := mainImpl(); err != nil {
		log.Fatal(err)
	}
}

func mainImpl() error {
	// Set up a signal-interruptible context
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	return commands.NewRootCmd().ExecuteContext(ctx)
}
