package main

import (
	"fmt"
	"io"
	"os"

	"github.com/standards-lab/go-web-service/internal/process"
)

const usage = `usage: db <command> [args]

commands:
	migrate  apply or inspect schema migrations (db migrate -help)
	seed     load the reference data
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return process.Usage(stderr, usage)
	}

	switch args[0] {
	case "-h", "-help", "--help", "help":
		return process.Usage(stdout, usage)
	case "migrate":
		return migrateCmd(args[1:], stdout, stderr)
	case "seed":
		return seedCmd(args[1:], stdout, stderr)
	default:
		return process.Usage(
			stderr,
			fmt.Sprintf("db: unknown command %q\n%s", args[0], usage),
		)
	}
}
