package main

import (
	"fmt"
	"io"
)

type cliCommand interface {
	Name() string
	Run(args []string) error
}

type commandFunc struct {
	name string
	run  func([]string) error
}

func (c commandFunc) Name() string {
	return c.name
}

func (c commandFunc) Run(args []string) error {
	return c.run(args)
}

type commandRegistry struct {
	commands map[string]cliCommand
}

func newCommandRegistry(out io.Writer) commandRegistry {
	registry := commandRegistry{commands: map[string]cliCommand{}}
	registry.register(commandFunc{name: "sample", run: runSample})
	registry.register(commandFunc{name: "inspect", run: runInspect})
	registry.register(commandFunc{name: "compare", run: runCompare})
	registry.register(commandFunc{name: "export", run: runExport})
	registry.register(commandFunc{name: "problems", run: runProblems})
	registry.register(commandFunc{name: "version", run: func([]string) error {
		printVersion(out)
		return nil
	}})
	registry.register(commandFunc{name: "help", run: func([]string) error {
		usage()
		return nil
	}})
	registry.register(commandFunc{name: "-h", run: func([]string) error {
		usage()
		return nil
	}})
	registry.register(commandFunc{name: "--help", run: func([]string) error {
		usage()
		return nil
	}})
	return registry
}

func (r commandRegistry) register(command cliCommand) {
	r.commands[command.Name()] = command
}

func (r commandRegistry) run(args []string) error {
	if len(args) == 0 {
		usage()
		return commandExitError{message: "missing command", code: 2}
	}
	command := r.commands[args[0]]
	if command == nil {
		return fmt.Errorf("unknown command %q", args[0])
	}
	return command.Run(args[1:])
}

type commandExitError struct {
	message string
	code    int
}

func (e commandExitError) Error() string {
	return e.message
}

func (e commandExitError) ExitCode() int {
	return e.code
}
