package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/app"
)

// main boots the Porcelain scaffold and exits with a non-zero status on failure.
func main() {
	if err := app.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
