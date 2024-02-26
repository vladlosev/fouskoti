package main

import (
	"os"
	"slices"

	"github.com/vladlosev/fouskoti/cmd"
)

func main() {
	var options cmd.RootCommandOptions
	rootCommand := cmd.NewRootCommand(&options)

	childCommand, _, _ := rootCommand.Find(os.Args[1:])
	if childCommand == rootCommand {
		os.Args = slices.Insert(os.Args, 1, cmd.ExpandCommandName)
	}

	err := rootCommand.Execute()
	if err != nil {
		os.Exit(1)
	}
}
