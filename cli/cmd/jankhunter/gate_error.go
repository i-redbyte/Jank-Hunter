package main

import "strings"

type gateError struct {
	failures []string
}

func (e gateError) Error() string {
	return "regression gate failed: " + strings.Join(e.failures, "; ")
}

func (e gateError) ExitCode() int {
	return 1
}
