package main

import (
	"fmt"
	"os"
)

func main() {
	extension := NewVSCodeExtension()
	rootCmd := extension.CreateRootCommand()

	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("Ошибка: %v\n", err)
		os.Exit(1)
	}
}
