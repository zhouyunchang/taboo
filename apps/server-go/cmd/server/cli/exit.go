package cli

import (
	"fmt"
	"log"
	"os"
)

// exitError carries a process exit code through cobra.Execute.
type exitError struct {
	code    int
	msg     string
	printed bool
}

func (e *exitError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return fmt.Sprintf("exit %d", e.code)
}

func exitf(code int, format string, args ...any) error {
	return &exitError{code: code, msg: fmt.Sprintf(format, args...)}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := err.(*exitError); ok {
		return e.code
	}
	return 1
}

func reportExecuteErr(err error) {
	if err == nil {
		return
	}
	if e, ok := err.(*exitError); ok {
		if e.msg != "" && !e.printed {
			if e.code == 2 {
				fmt.Fprintln(os.Stderr, e.msg)
			} else {
				log.Print(e.msg)
			}
		}
		return
	}
	fmt.Fprintln(os.Stderr, err.Error())
}

func fatalErr(err error) {
	if err == nil {
		return
	}
	if e, ok := err.(*exitError); ok && e.msg != "" {
		log.Fatal(e.msg)
	}
	log.Fatal(err)
}
