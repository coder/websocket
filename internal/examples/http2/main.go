package main

import (
	"flag"
	"fmt"
	"os"
)

// main dispatches to subcommands and delegates implementation details to
// serverMain and clientMain (defined in server.go and client.go).
func main() {
	prog := os.Args[0]
	fs := flag.NewFlagSet(prog, flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage:
  %[1]s <command> [options]

Options:
`, prog)
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), `
Commands:
  server    Run the HTTP/2 WebSocket echo server
  client    Connect to a server over HTTP/2 and echo a message
  help      Show help for a command (e.g. "%[1]s help server")

Run:
  %[1]s server -h
  %[1]s client -h
for command-specific options.
`, prog)
	}
	err := fs.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing flags: %v\n", err)
		os.Exit(1)
	}

	switch cmd := fs.Arg(0); cmd {
	case "server":
		if err := serverMain(prog, fs.Args()[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "server: error: %v\n", err)
			os.Exit(1)
		}
	case "client":
		if err := clientMain(prog, fs.Args()[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "client: error: %v\n", err)
			os.Exit(1)
		}
	case "", "help":
		switch fs.Arg(1) {
		case "server":
			_ = serverMain(prog, []string{"-h"})
		case "client":
			_ = clientMain(prog, []string{"-h"})
		case "":
		default:
			fmt.Fprintf(os.Stderr, "error: unknown command %q\n\n", fs.Arg(1))
		}
		fs.Usage()
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n\n", fs.Arg(0))
		fs.Usage()
		os.Exit(1)
	}
}
