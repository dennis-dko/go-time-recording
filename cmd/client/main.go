package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/dennis-dko/go-time-recording/internal/interface/cli"
)

// version is the release this client was built for, set the way the web
// binary's is - -ldflags "-X main.version=v1.2.3" - and from the same tag, so
// the two answer --version alike when they belong together.
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println(version)
		os.Exit(0)
	}

	// Ctrl-C cancels the request it is waiting on.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	status := cli.Run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)

	stop()
	os.Exit(status)
}
