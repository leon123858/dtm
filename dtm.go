package main

import (
	"dtm/cmd"
	"fmt"
	"log"
	"os"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		_, err := fmt.Fprintf(os.Stderr, "wrong execute: %v\n", err)
		if err != nil {
			log.Fatal(err)
			return
		}
		os.Exit(1)
	}
}
