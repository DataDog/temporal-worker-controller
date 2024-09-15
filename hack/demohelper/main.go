package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/fatih/color"
)

var (
	faintColor   = color.New(color.Faint)
	commandColor = color.New(color.FgHiBlue)
)

var (
	skaffoldRunCmd      = newCommand(`skaffold run --profile demo`)
	gitResetWorkflowCmd = newCommand(`git checkout internal/demo/worker/workflow.go`)
)

type demoCommand struct {
	command string
	// If set, the command will automatically be killed after this duration.
	watchDuration time.Duration
}

func newCommand(command string) demoCommand {
	return demoCommand{command: command, watchDuration: 0}
}

func (c demoCommand) WithWatchDuration(d time.Duration) demoCommand {
	return demoCommand{
		command:       c.command,
		watchDuration: d,
	}
}

type demoStep struct {
	description string
	commands    []demoCommand
}

func (ds demoStep) Run(ctx context.Context) error {
	_, _ = faintColor.Printf("# %s\n", ds.description)
	for _, c := range ds.commands {
		// Print the command before running it
		fmt.Printf("$ %s\n", commandColor.Sprint(c.command))
		// Run the command
		if err := func() error {
			var (
				commandCtx = ctx
				isWatch    = c.watchDuration > 0
			)
			if isWatch {
				c, cancel := context.WithTimeout(ctx, c.watchDuration)
				defer cancel()
				commandCtx = c
			}

			cmd := exec.CommandContext(commandCtx, "sh", "-c", c.command)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			err := cmd.Run()
			if isWatch {
				return ignoreExecKillError(err)
			}
			return err
		}(); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	steps := []demoStep{
		{
			"Ensure worker is up to date",
			[]demoCommand{
				newCommand(`skaffold run --profile demo`),
				newCommand(`kubectl get deployments,pods`),
			},
		},
		{
			"Switch to workflow.Sleep using a patch/version check",
			[]demoCommand{newCommand(`git apply ./internal/demo/changes/version-gate.patch`)},
		},
		{
			"Remove the patch/version check",
			[]demoCommand{
				gitResetWorkflowCmd,
				newCommand(`git apply ./internal/demo/changes/no-version-gate.patch`),
			},
		},
		{
			"Deploy the change to workflow.Sleep",
			[]demoCommand{
				newCommand(`git add internal/demo/worker/workflow.go`),
				newCommand(`git commit -m "Use workflow.Sleep instead of time.Sleep (no version gate)"`),
				//newCommand(`git push`),
				skaffoldRunCmd,
				newCommand(`kubectl get deployments -f`).WithWatchDuration(5 * time.Second),
			},
		},
		{
			"Inspect worker status: the deprecated version should still be reachable.",
			[]demoCommand{
				newCommand(`kubectl get -o yaml temporalworker sample | yq '.status' | grep -v -E 'apiVersion|resourceVersion|kind|uid|namespace|deployment|name|versionConflictToken' | yq`),
			},
		},
		{
			"Revert the changes",
			[]demoCommand{
				newCommand(`git reset HEAD~1`),
				gitResetWorkflowCmd,
				skaffoldRunCmd,
			},
		},
	}

	runDemo(steps)
}

func runDemo(steps []demoStep) {
	for _, s := range steps {
		// Print the description
		_, _ = faintColor.Print("# Next: ", s.description, " [ENTER] ")

		// wait for ENTER key
		if _, err := fmt.Scanln(); err != nil {
			log.Fatalf("Error reading input: %v", err)
		}

		// Clear the console
		//if err := clearConsole(); err != nil {
		//	log.Fatalf("Error clearing console: %v", err)
		//}

		// Run the command
		if err := s.Run(context.Background()); err != nil {
			log.Fatalf("Error running command: %v", err)
		}
	}
	_, _ = faintColor.Println("# Demo complete!")
}

func clearConsole() error {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

func ignoreExecKillError(err error) error {
	// Extract the exit code
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == -1 {
			return nil
		}
		//return nil
	}
	return err
}
