package main

import (
	"os"

	"github.com/daum110715/daumini-agent/internal/cli"
	"github.com/daum110715/daumini-agent/internal/webui"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], webui.FS()))
}
