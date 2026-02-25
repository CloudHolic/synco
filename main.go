package main

import (
	"os"
	"synco/cmd"
)

func main() {
	if len(os.Args) == 1 {
		os.Args = append(os.Args, "watch")
	}
	cmd.Execute()
}
