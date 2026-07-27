package main

import (
	"fmt"
	"io"
	"os"

	"github.com/m-cain/autoboard/internal/contracts"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(arguments []string, stderr io.Writer) int {
	check := len(arguments) == 3 && arguments[0] == "--check"
	if (!check && len(arguments) != 2) || (check && len(arguments) != 3) {
		fmt.Fprintln(
			stderr,
			"usage: autoboard-schema [--check] OUTPUT_DIRECTORY TYPESCRIPT_MODULE",
		)
		return 2
	}
	if check {
		arguments = arguments[1:]
	}
	var err error
	if check {
		err = contracts.Check(arguments[0], arguments[1])
	} else {
		err = contracts.Write(arguments[0], arguments[1])
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
