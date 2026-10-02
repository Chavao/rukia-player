package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Chavao/rukia-player/internal/app"
)

func main() {
	ctx := context.Background()
	if err := app.Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
