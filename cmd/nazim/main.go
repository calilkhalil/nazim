// nazim manages services across multiple operating systems.
//
// It allows creating and managing services (scripts/commands) that can be
// executed on system startup or at regular intervals. Works on Windows, Linux, and macOS.
//
// Usage:
//
//	nazim [command] [flags]
//
// Commands:
//
//	add       add a new service
//	list      list all services
//	remove    remove a service
//	start     start a service
//	stop      stop a service
//
// Flags:
//
//	-h, --help    show help
//	-v, --verbose enable verbose output
//
// Environment:
//
//	NAZIM_VERBOSE  set to "1" for verbose output
//	XDG_CONFIG_HOME config directory base (default: ~/.config on Linux/macOS, %APPDATA% on Windows)
//
// Examples:
//
//	nazim add --name "backup" --command "backup.sh" --interval "1h"
//	nazim add --name "startup-script" --command "init.sh" --on-startup
//	nazim list
//	nazim remove backup
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"nazim/internal/cli"
	"nazim/internal/config"
)

const (
	exitOK    = 0
	exitError = 1
)

// Flags holds parsed command-line flags.
type Flags struct {
	Verbose   bool
	Help      bool
	Name      string
	Command   string
	Args      string
	WorkDir   string
	OnStartup bool
	Interval  string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return exitError
	}

	command := args[0]
	remainingArgs := args[1:]

	// Handle help flag
	if command == "-h" || command == "--help" || command == "help" {
		printUsage(stdout)
		return exitOK
	}

	// Setup context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Parse flags from remaining args
	flags, cmdArgs, err := parseFlags(remainingArgs)
	if err != nil {
		fmt.Fprintf(stderr, "nazim: %v\n", err)
		return exitError
	}

	// Handle verbose from env if not set via flag
	verbose := flags.Verbose || os.Getenv("NAZIM_VERBOSE") == "1"

	cfg, err := config.New()
	if err != nil {
		fmt.Fprintf(stderr, "nazim: %v\n", err)
		return exitError
	}

	cliHandler := cli.New(cfg)

	// Route to appropriate command
	switch command {
	case "add":
		addFlags := &cli.Flags{
			Name:      flags.Name,
			Command:   flags.Command,
			Args:      flags.Args,
			WorkDir:   flags.WorkDir,
			OnStartup: flags.OnStartup,
			Interval:  flags.Interval,
		}
		if err := cliHandler.Add(ctx, addFlags, verbose); err != nil {
			fmt.Fprintf(stderr, "nazim: %v\n", err)
			return exitError
		}
		return exitOK

	case "list":
		if err := cliHandler.List(ctx, verbose); err != nil {
			fmt.Fprintf(stderr, "nazim: %v\n", err)
			return exitError
		}
		return exitOK

	case "remove":
		if len(cmdArgs) == 0 {
			fmt.Fprintf(stderr, "nazim: remove requires a service name\n")
			return exitError
		}
		// Join all remaining args to support service names with spaces
		serviceName := strings.Join(cmdArgs, " ")
		if err := cliHandler.Remove(ctx, serviceName, verbose); err != nil {
			fmt.Fprintf(stderr, "nazim: %v\n", err)
			return exitError
		}
		return exitOK

	case "start":
		if len(cmdArgs) == 0 {
			fmt.Fprintf(stderr, "nazim: start requires a service name\n")
			return exitError
		}
		// Join all remaining args to support service names with spaces
		serviceName := strings.Join(cmdArgs, " ")
		if err := cliHandler.Start(ctx, serviceName, verbose); err != nil {
			fmt.Fprintf(stderr, "nazim: %v\n", err)
			return exitError
		}
		return exitOK

	case "stop":
		if len(cmdArgs) == 0 {
			fmt.Fprintf(stderr, "nazim: stop requires a service name\n")
			return exitError
		}
		// Join all remaining args to support service names with spaces
		serviceName := strings.Join(cmdArgs, " ")
		if err := cliHandler.Stop(ctx, serviceName, verbose); err != nil {
			fmt.Fprintf(stderr, "nazim: %v\n", err)
			return exitError
		}
		return exitOK

	case "status", "info":
		if len(cmdArgs) == 0 {
			fmt.Fprintf(stderr, "nazim: %s requires a service name\n", command)
			return exitError
		}
		// Join all remaining args to support service names with spaces
		serviceName := strings.Join(cmdArgs, " ")
		if err := cliHandler.Status(ctx, serviceName, verbose); err != nil {
			fmt.Fprintf(stderr, "nazim: %v\n", err)
			return exitError
		}
		return exitOK

	case "edit":
		if len(cmdArgs) == 0 {
			fmt.Fprintf(stderr, "nazim: edit requires a service name\n")
			return exitError
		}
		// Join all remaining args to support service names with spaces
		serviceName := strings.Join(cmdArgs, " ")
		editFlags := &cli.Flags{
			Name:      flags.Name,
			Command:   flags.Command,
			Args:      flags.Args,
			WorkDir:   flags.WorkDir,
			OnStartup: flags.OnStartup,
			Interval:  flags.Interval,
		}
		if err := cliHandler.Edit(ctx, serviceName, editFlags, verbose); err != nil {
			fmt.Fprintf(stderr, "nazim: %v\n", err)
			return exitError
		}
		return exitOK

	case "enable":
		if len(cmdArgs) == 0 {
			fmt.Fprintf(stderr, "nazim: enable requires a service name\n")
			return exitError
		}
		// Join all remaining args to support service names with spaces
		serviceName := strings.Join(cmdArgs, " ")
		if err := cliHandler.Enable(ctx, serviceName, verbose); err != nil {
			fmt.Fprintf(stderr, "nazim: %v\n", err)
			return exitError
		}
		return exitOK

	case "disable":
		if len(cmdArgs) == 0 {
			fmt.Fprintf(stderr, "nazim: disable requires a service name\n")
			return exitError
		}
		// Join all remaining args to support service names with spaces
		serviceName := strings.Join(cmdArgs, " ")
		if err := cliHandler.Disable(ctx, serviceName, verbose); err != nil {
			fmt.Fprintf(stderr, "nazim: %v\n", err)
			return exitError
		}
		return exitOK

	default:
		fmt.Fprintf(stderr, "nazim: unknown command: %s\n", command)
		printUsage(stderr)
		return exitError
	}
}

func parseFlags(args []string) (*Flags, []string, error) {
	fs := flag.NewFlagSet("nazim", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // Handle errors manually

	flags := &Flags{}

	// Add command flags
	fs.StringVar(&flags.Name, "name", "", "")
	fs.StringVar(&flags.Name, "n", "", "")
	fs.StringVar(&flags.Command, "command", "", "")
	fs.StringVar(&flags.Command, "c", "", "")
	fs.StringVar(&flags.Args, "args", "", "")
	fs.StringVar(&flags.Args, "a", "", "")
	fs.StringVar(&flags.WorkDir, "workdir", "", "")
	fs.StringVar(&flags.WorkDir, "w", "", "")
	fs.BoolVar(&flags.OnStartup, "on-startup", false, "")
	fs.StringVar(&flags.Interval, "interval", "", "")
	fs.StringVar(&flags.Interval, "i", "", "")

	// Global flags
	fs.BoolVar(&flags.Verbose, "v", false, "")
	fs.BoolVar(&flags.Verbose, "verbose", false, "")
	fs.BoolVar(&flags.Help, "h", false, "")
	fs.BoolVar(&flags.Help, "help", false, "")

	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}

	return flags, fs.Args(), nil
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `nazim - Multi-OS Service Manager

Usage: nazim [command] [options]

Commands:
  add <options>     add a new service
  list              list all services
  status <name>     show detailed service information
  edit <name>       update an existing service
  remove <name>     remove a service
  start <name>      start a service manually
  stop <name>       stop a service
  enable <name>     enable a service
  disable <name>    disable a service

Add Options:
  -n, --name <name>        service name (required)
  -c, --command <cmd>      command or script to execute (required)
                           use "write" or "edit" to open editor interactively
  -a, --args <args>        arguments for the command
  -w, --workdir <dir>      working directory
      --on-startup         run on system startup
  -i, --interval <dur>      execution interval (e.g., 5m, 1h, 30s)

Global Options:
  -v, --verbose     enable verbose output
  -h, --help        show this help

Environment:
  NAZIM_VERBOSE     set to "1" for verbose output
  XDG_CONFIG_HOME   config directory base (default: ~/.config on Linux/macOS, %APPDATA% on Windows)

Examples:
  # Simple one-liner command
  nazim add --name cleanup --command "rm -rf /tmp/old_files" --interval 1h

  # Service with a script file
  nazim add --name backup --command backup.sh --interval 1h

  # Service that runs on startup
  nazim add --name init --command init.sh --on-startup

  # Interactive mode: open editor to write script
  nazim add --name myscript --command write --interval 30m

  # Command with arguments
  nazim add --name processor --command python --args "script.py --verbose" --interval 30m

  # List all services
  nazim list

  # Remove a service
  nazim remove backup

  # Start a service manually
  nazim start backup

  # Stop a service
  nazim stop backup

  # Show service status
  nazim status backup

  # Edit a service
  nazim edit backup --interval 2h

  # Enable/disable a service
  nazim enable backup
  nazim disable backup

Interval Format:
  Use s (seconds), m (minutes), h (hours), or d (days)
  Examples: 30s, 5m, 1h, 24h

Config: ~/.config/nazim/services.yaml (or equivalent on Windows/macOS)
`)
}
