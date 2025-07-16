package main

import (
	v1 "github.com/dennis-dko/go-time-recording/internal/interface/api/v1"
	"gofr.dev/pkg/gofr"
)

func main() {
	app := gofr.New()

	err := v1.RegisterRoutes(app)
	if err != nil {
		panic("FAILED")
	}

	app.Run()
}
