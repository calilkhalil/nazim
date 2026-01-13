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
//	enable    enable a service
//	disable   disable a service
//	run       run a service immediately
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

	"github.com/calilkhalil/nazim/internal/cli"
	"github.com/calilkhalil/nazim/internal/config"
)

// Version information (set by build flags)
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
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
	OnLogon   bool
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

	// Handle version flag
	if command == "--version" || command == "version" {
		fmt.Fprintf(stdout, "nazim version %s (commit: %s, built: %s)\n", version, commit, buildDate)
		return exitOK
	}

	// Handle help flag
	if command == "-h" || command == "--help" || command == "help" {
		printUsage(stdout)
		return exitOK
	}

	// Setup context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	remainingArgs = preprocessArgs(remainingArgs, command)
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

	return handleCommand(ctx, command, flags, cmdArgs, cliHandler, verbose, stderr)
}

func handleCommand(ctx context.Context, command string, flags *Flags, cmdArgs []string, cliHandler *cli.CLI, verbose bool, stderr io.Writer) int {
	switch command {
	case "add":
		return handleAdd(ctx, flags, cliHandler, verbose, stderr)
	case "list":
		return handleList(ctx, cliHandler, verbose, stderr)
	case "remove":
		return handleRemove(ctx, cmdArgs, cliHandler, verbose, stderr)
	case "enable":
		return handleEnable(ctx, cmdArgs, cliHandler, verbose, stderr)
	case "disable":
		return handleDisable(ctx, cmdArgs, cliHandler, verbose, stderr)
	case "run":
		return handleRun(ctx, cmdArgs, cliHandler, verbose, stderr)
	case "status", "info":
		return handleStatus(ctx, command, cmdArgs, cliHandler, verbose, stderr)
	case "edit":
		return handleEdit(ctx, cmdArgs, flags, cliHandler, verbose, stderr)
	default:
		fmt.Fprintf(stderr, "nazim: unknown command: %s\n", command)
		printUsage(stderr)
		return exitError
	}
}

func handleAdd(ctx context.Context, flags *Flags, cliHandler *cli.CLI, verbose bool, stderr io.Writer) int {
	addFlags := &cli.Flags{
		Name:      flags.Name,
		Command:   flags.Command,
		Args:      flags.Args,
		WorkDir:   flags.WorkDir,
		OnStartup: flags.OnStartup,
		OnLogon:   flags.OnLogon,
		Interval:  flags.Interval,
	}
	if err := cliHandler.Add(ctx, addFlags, verbose); err != nil {
		fmt.Fprintf(stderr, "nazim: %v\n", err)
		return exitError
	}
	return exitOK
}

func handleList(ctx context.Context, cliHandler *cli.CLI, verbose bool, stderr io.Writer) int {
	if err := cliHandler.List(ctx, verbose); err != nil {
		fmt.Fprintf(stderr, "nazim: %v\n", err)
		return exitError
	}
	return exitOK
}

func handleRemove(ctx context.Context, cmdArgs []string, cliHandler *cli.CLI, verbose bool, stderr io.Writer) int {
	if len(cmdArgs) == 0 {
		fmt.Fprintf(stderr, "nazim: remove requires a service name\n")
		return exitError
	}
	serviceName := strings.Join(cmdArgs, " ")
	if err := cliHandler.Remove(ctx, serviceName, verbose); err != nil {
		fmt.Fprintf(stderr, "nazim: %v\n", err)
		return exitError
	}
	return exitOK
}

func handleRun(ctx context.Context, cmdArgs []string, cliHandler *cli.CLI, verbose bool, stderr io.Writer) int {
	if len(cmdArgs) == 0 {
		fmt.Fprintf(stderr, "nazim: run requires a service name\n")
		return exitError
	}
	serviceName := strings.Join(cmdArgs, " ")
	if err := cliHandler.Run(ctx, serviceName, verbose); err != nil {
		fmt.Fprintf(stderr, "nazim: %v\n", err)
		return exitError
	}
	return exitOK
}

func handleStatus(ctx context.Context, command string, cmdArgs []string, cliHandler *cli.CLI, verbose bool, stderr io.Writer) int {
	if len(cmdArgs) == 0 {
		fmt.Fprintf(stderr, "nazim: %s requires a service name\n", command)
		return exitError
	}
	serviceName := strings.Join(cmdArgs, " ")
	if err := cliHandler.Status(ctx, serviceName, verbose); err != nil {
		fmt.Fprintf(stderr, "nazim: %v\n", err)
		return exitError
	}
	return exitOK
}

func handleEdit(ctx context.Context, cmdArgs []string, flags *Flags, cliHandler *cli.CLI, verbose bool, stderr io.Writer) int {
	var serviceName string

	// Prefer --name flag if provided
	if flags.Name != "" {
		serviceName = flags.Name
	} else if len(cmdArgs) > 0 {
		serviceName = strings.Join(cmdArgs, " ")
	} else {
		fmt.Fprintf(stderr, "nazim: edit requires a service name (use --name or provide as argument)\n")
		return exitError
	}

	editFlags := &cli.Flags{
		Name:      flags.Name,
		Command:   flags.Command,
		Args:      flags.Args,
		WorkDir:   flags.WorkDir,
		OnStartup: flags.OnStartup,
		OnLogon:   flags.OnLogon,
		Interval:  flags.Interval,
	}
	if err := cliHandler.Edit(ctx, serviceName, editFlags, verbose); err != nil {
		fmt.Fprintf(stderr, "nazim: %v\n", err)
		return exitError
	}
	return exitOK
}

func handleEnable(ctx context.Context, cmdArgs []string, cliHandler *cli.CLI, verbose bool, stderr io.Writer) int {
	if len(cmdArgs) == 0 {
		fmt.Fprintf(stderr, "nazim: enable requires a service name\n")
		return exitError
	}
	serviceName := strings.Join(cmdArgs, " ")
	if err := cliHandler.Enable(ctx, serviceName, verbose); err != nil {
		fmt.Fprintf(stderr, "nazim: %v\n", err)
		return exitError
	}
	return exitOK
}

func handleDisable(ctx context.Context, cmdArgs []string, cliHandler *cli.CLI, verbose bool, stderr io.Writer) int {
	if len(cmdArgs) == 0 {
		fmt.Fprintf(stderr, "nazim: disable requires a service name\n")
		return exitError
	}
	serviceName := strings.Join(cmdArgs, " ")
	if err := cliHandler.Disable(ctx, serviceName, verbose); err != nil {
		fmt.Fprintf(stderr, "nazim: %v\n", err)
		return exitError
	}
	return exitOK
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
	fs.BoolVar(&flags.OnLogon, "on-logon", false, "")
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

func preprocessArgs(args []string, command string) []string {
	// For edit command, reorder args to put flags before positional arguments
	if command == "edit" {
		return reorderArgsForEdit(args)
	}

	// For add command, handle multi-word values for specific flags
	if command != "add" {
		return args
	}

	// Known nazim flags that we should recognize
	knownFlags := map[string]bool{
		"--name": true, "-n": true,
		"--command": true, "-c": true,
		"--args": true, "-a": true,
		"--workdir": true, "-w": true,
		"--interval": true, "-i": true,
		"--on-startup": true,
		"--on-logon":   true,
		"--verbose":    true, "-v": true,
		"--help": true, "-h": true,
	}

	result := make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		currentFlag := args[i]

		// Flags that may contain complex values (with spaces or internal flags)
		if currentFlag == "--name" || currentFlag == "-n" ||
			currentFlag == "--command" || currentFlag == "-c" ||
			currentFlag == "--args" || currentFlag == "-a" {

			result = append(result, currentFlag)
			i++

			// Accumulate value parts until we hit a known nazim flag
			if i < len(args) && !strings.HasPrefix(args[i], "-") {
				var valueParts []string
				valueParts = append(valueParts, args[i])
				i++

				// Continue accumulating until we find a known nazim flag
				for i < len(args) {
					if knownFlags[args[i]] {
						break
					}
					valueParts = append(valueParts, args[i])
					i++
				}

				result = append(result, strings.Join(valueParts, " "))
			}
		} else {
			result = append(result, args[i])
			i++
		}
	}
	return result
}

// reorderArgsForEdit moves positional arguments (service name) to the end
// so flags can be parsed correctly by Go's flag package
func reorderArgsForEdit(args []string) []string {
	if len(args) == 0 {
		return args
	}

	// If first arg is not a flag, move it to the end
	if !strings.HasPrefix(args[0], "-") {
		serviceName := args[0]
		remaining := args[1:]
		return append(remaining, serviceName)
	}

	return args
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
  enable <name>     enable a service (allows scheduled execution)
  disable <name>    disable a service (prevents scheduled execution)
  run <name>        execute a service immediately
  version           show version information

Add Options:
  -n, --name <name>        service name (required)
  -c, --command <cmd>      command or script to execute (required)
                           use "write" or "edit" to open editor interactively
  -a, --args <args>        arguments for the command
  -w, --workdir <dir>      working directory
      --on-startup         run at system boot (as SYSTEM, no user context)
      --on-logon           run at user logon (as current user, has HKCU access)
  -i, --interval <dur>      execution interval (e.g., 5m, 1h, 30s)

Global Options:
  -v, --verbose        enable verbose output
  -h, --help           show this help
      --version        show version information

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

  # Show service status
  nazim status backup

  # Edit a service
  nazim edit backup --interval 2h

  # Enable/disable a service
  nazim enable backup
  nazim disable backup

  # Run a service immediately
  nazim run backup

Interval Format:
  Use s (seconds), m (minutes), h (hours), or d (days)
  Examples: 30s, 5m, 1h, 24h

Config: ~/.config/nazim/services.yaml (or equivalent on Windows/macOS)
`)
}
