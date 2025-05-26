package main

import (
	"dtm/cmd"
	"fmt"
	"os"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "執行錯誤: %v\n", err)
		os.Exit(1)
	}
}
