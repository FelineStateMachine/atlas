// Command testgate runs `go test` and refuses to call a skipped test a pass.
//
// The golden harness this tree grew up under had one blind spot: `go test`
// exits zero when every test in a package skips, so a gate whose inputs were
// missing said PASS while judging nothing. The pipeline gate did exactly that
// on every CI run, and the derive gate did it silently for weeks after its
// reference tool left the tree. This command is the structural fix: a test
// that does not run is a finding, not a pass.
//
// Usage:
//
//	go run ./tools/testgate [go test flags] [packages]
//
// Everything after the command name is handed to `go test -json` verbatim,
// so `go run ./tools/testgate -race ./...` means what it looks like. The
// output is the ordinary `go test` narration, reassembled from the JSON
// stream; on top of it, any test-level skip fails the run with the list of
// what skipped and why that is not allowed here.
//
// A package with no test files still reports itself as skipped in the JSON
// stream with no test name attached; that is the one skip this gate accepts,
// because there is nothing there to have judged.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// event is the subset of test2json's record this gate reads.
type event struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

func main() {
	args := append([]string{"test", "-json"}, os.Args[1:]...)
	if len(os.Args) == 1 {
		args = append(args, "./...")
	}
	cmd := exec.Command("go", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "testgate:", err)
		os.Exit(1)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "testgate:", err)
		os.Exit(1)
	}

	var skipped []string
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var ev event
		if err := json.Unmarshal(line, &ev); err != nil {
			// Non-JSON lines happen when a build fails; pass them through.
			os.Stdout.Write(append(line, '\n'))
			continue
		}
		if ev.Action == "output" {
			os.Stdout.WriteString(ev.Output)
		}
		if ev.Action == "skip" && ev.Test != "" {
			skipped = append(skipped, ev.Package+" "+ev.Test)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "testgate:", err)
		os.Exit(1)
	}

	code := 0
	if err := cmd.Wait(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			fmt.Fprintln(os.Stderr, "testgate:", err)
			code = 1
		}
	}
	if len(skipped) > 0 {
		fmt.Fprintf(os.Stderr, "\ntestgate: %d test(s) skipped; a required suite may not skip:\n", len(skipped))
		for _, s := range skipped {
			fmt.Fprintln(os.Stderr, "  ", s)
		}
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
