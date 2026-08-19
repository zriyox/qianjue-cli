package main

import (
	"os"

	"github.com/zriyox/qianjue-cli/internal/cmd"
)

func main() {
	os.Exit(cmd.Execute(os.Args[1:]))
}
