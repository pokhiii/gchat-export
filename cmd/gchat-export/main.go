package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/OWNER/gchat-export/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Execute(ctx, version, os.Stdout, os.Stderr, os.Args[1:])
	stop()
	os.Exit(code)
}
