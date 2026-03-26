package main

import (
	"log"

	"github.com/negbie/telefonist/pkg/telefonist"
)

func main() {
	if err := telefonist.Run(); err != nil {
		log.Fatalf("telefonist failed: %v", err)
	}
}
