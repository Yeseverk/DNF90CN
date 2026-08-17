// Command control is the native DNF90 local deployment controller.
//
// It intentionally does not invoke PowerShell. All generated runtime files,
// process ownership checks, portable MySQL lifecycle, and client compatibility
// checks are implemented in this command.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const commandTimeout = 10 * time.Minute

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printUsage(stdout)
		return 0
	}

	paths, err := discoverPaths()
	if err != nil {
		fmt.Fprintln(stderr, "FAILED:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	releaseLock, err := acquireControllerLock(ctx, paths.projectRoot)
	if err != nil {
		fmt.Fprintln(stderr, "FAILED:", err)
		return 1
	}
	defer releaseLock()
	if err := recoverBuiltBinarySet(paths); err != nil {
		fmt.Fprintln(stderr, "FAILED: recover interrupted binary installation:", err)
		return 1
	}
	controller := newController(paths, stdout, stderr)

	var runErr error
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "start":
		fs := newFlagSet("start", stderr)
		rebuild := fs.Bool("rebuild", false, "rebuild server and doctor before starting")
		if runErr = parseFlags(fs, args[1:]); runErr == nil {
			runErr = controller.start(ctx, *rebuild)
		}
	case "stop":
		fs := newFlagSet("stop", stderr)
		keepDatabase := fs.Bool("keep-database", false, "leave the bundled MySQL process running")
		if runErr = parseFlags(fs, args[1:]); runErr == nil {
			runErr = controller.stop(ctx, *keepDatabase)
		}
	case "status":
		fs := newFlagSet("status", stderr)
		if runErr = parseFlags(fs, args[1:]); runErr == nil {
			runErr = controller.status(ctx)
		}
	case "build":
		fs := newFlagSet("build", stderr)
		force := fs.Bool("force", true, "rebuild even when binaries already exist")
		if runErr = parseFlags(fs, args[1:]); runErr == nil {
			runErr = controller.build(ctx, *force)
		}
	case "check":
		fs := newFlagSet("check", stderr)
		skipDatabase := fs.Bool("skip-database", false, "skip MySQL connectivity validation")
		skipPorts := fs.Bool("skip-ports", false, "skip listener availability validation")
		checkClient := fs.Bool("client", false, "require and validate the configured client directory")
		if runErr = parseFlags(fs, args[1:]); runErr == nil {
			runErr = controller.check(ctx, checkOptions{
				skipDatabase: *skipDatabase,
				skipPorts:    *skipPorts,
				checkClient:  *checkClient,
			})
		}
	case "configure-client":
		fs := newFlagSet("configure-client", stderr)
		directory := fs.String("directory", "", "absolute directory containing DNF.exe or NoPack.exe")
		if runErr = parseFlags(fs, args[1:]); runErr == nil {
			runErr = controller.configureClient(*directory)
		}
	case "launch-client":
		fs := newFlagSet("launch-client", stderr)
		clientDirectory := fs.String("client-directory", "", "override client.directory from instance.json")
		multiInstance := fs.Bool("multi-instance", false, "launch an additional client using the audited current-EXE instance compatibility")
		username := fs.String("username", "", "authenticate and bind this client to a local account")
		passwordStdin := fs.Bool("password-stdin", false, "read one password line from stdin")
		if runErr = parseFlags(fs, args[1:]); runErr == nil {
			password := ""
			if strings.TrimSpace(*username) != "" {
				if !*passwordStdin {
					runErr = errors.New("launch-client with --username requires --password-stdin")
				} else {
					password, runErr = readLocalAccountPassword(controller.stdin)
				}
			} else if *passwordStdin {
				runErr = errors.New("--password-stdin requires --username")
			}
			if runErr == nil {
				runErr = controller.launchClient(
					ctx,
					*clientDirectory,
					*multiInstance,
					*username,
					password,
				)
			}
		}
	case "account":
		runErr = controller.account(ctx, args[1:])
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}

	if runErr != nil {
		if errors.Is(runErr, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "FAILED:", runErr)
		return 1
	}
	return 0
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `DNF90 native local server controller

Usage:
  DNF90Control.exe start [--rebuild]
  DNF90Control.exe stop [--keep-database]
  DNF90Control.exe status
  DNF90Control.exe build [--force=true]
  DNF90Control.exe check [--skip-database] [--skip-ports] [--client]
  DNF90Control.exe configure-client --directory PATH
  DNF90Control.exe launch-client [--client-directory PATH] [--multi-instance] [--username NAME --password-stdin]
  DNF90Control.exe account register --username NAME --password-stdin
  DNF90Control.exe account login --username NAME --password-stdin
  DNF90Control.exe account list

Project discovery starts at the current directory and executable directory.
Set DNF90_PROJECT_ROOT to override it.`)
}
