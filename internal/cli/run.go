package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

const version = "0.0.0-dev"

// Run executes the Phase 0 command scaffold. Operational handlers will replace
// the unavailable response one vertical slice at a time.
func Run(args []string, stdout, stderr io.Writer) int {
	return RunContext(context.Background(), args, stdout, stderr)
}

func RunContext(execution context.Context, args []string, stdout, stderr io.Writer) int {
	commandContext, args, err := parseGlobals(args)
	if err != nil {
		fmt.Fprintf(stderr, "waldo: %v\n", err)
		return 2
	}
	commandContext.Execution = execution
	root := commandTree()
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printHelp(stdout, root, nil)
		return 0
	}
	if args[0] == "--version" || args[0] == "version" {
		fmt.Fprintf(stdout, "waldo %s\n", version)
		return 0
	}

	command := root
	path := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == "help" || arg == "--help" || arg == "-h" {
			printHelp(stdout, command, path)
			return 0
		}
		next, ok := child(command, arg)
		if !ok {
			fmt.Fprintf(stderr, "waldo: unknown command %q under %q\n", arg, commandPath(path))
			fmt.Fprintf(stderr, "Run %q for available commands.\n", commandPath(append(path, "--help")))
			return 2
		}
		command = next
		path = append(path, arg)
		if len(command.Children) == 0 {
			remaining := args[i+1:]
			if len(remaining) == 1 && (remaining[0] == "help" || remaining[0] == "--help" || remaining[0] == "-h") {
				printHelp(stdout, command, path)
				return 0
			}
			if command.Handler == nil {
				if len(remaining) > 0 {
					fmt.Fprintf(stderr, "waldo: %q does not accept arguments yet\n", commandPath(path))
					return 2
				}
				fmt.Fprintf(stderr, "%s is not available yet; this command has not reached its implementation phase.\n", commandPath(path))
				return 1
			}
			if err := command.Handler(commandContext, remaining, stdout, stderr); err != nil {
				var usage usageError
				if errors.As(err, &usage) {
					fmt.Fprintf(stderr, "waldo: %v\n", usage)
					return 2
				}
				fmt.Fprintf(stderr, "waldo: %v\n", err)
				return 1
			}
			return 0
		}
	}

	printHelp(stdout, command, path)
	return 0
}

type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

func parseGlobals(args []string) (Context, []string, error) {
	var context Context
	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json":
			context.JSON = true
		case arg == "--index":
			if i+1 == len(args) {
				return Context{}, nil, fmt.Errorf("--index needs a checkout path")
			}
			i++
			context.IndexPath = args[i]
		case strings.HasPrefix(arg, "--index="):
			context.IndexPath = strings.TrimPrefix(arg, "--index=")
		default:
			remaining = append(remaining, arg)
		}
	}
	return context, remaining, nil
}

func child(parent Command, name string) (Command, bool) {
	for _, candidate := range parent.Children {
		if candidate.Name == name {
			return candidate, true
		}
	}
	return Command{}, false
}

func printHelp(w io.Writer, command Command, path []string) {
	name := commandPath(path)
	fmt.Fprintf(w, "%s - %s\n\n", name, command.Summary)
	if len(command.Children) == 0 {
		usage := command.Usage
		if usage == "" {
			usage = name
		}
		fmt.Fprintf(w, "Usage:\n  %s\n", usage)
		return
	}
	fmt.Fprintf(w, "Usage:\n  %s <command>\n\nCommands:\n", name)
	for _, candidate := range command.Children {
		fmt.Fprintf(w, "  %-12s %s\n", candidate.Name, candidate.Summary)
	}
	fmt.Fprintf(w, "\nRun %q for details about a command.\n", name+" <command> --help")
}

func commandPath(parts []string) string {
	if len(parts) == 0 {
		return "waldo"
	}
	return "waldo " + strings.Join(parts, " ")
}
