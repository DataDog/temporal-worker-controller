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
	//fakePromptUser = fmt.Sprintf("%s%s%s",
	//	color.New(color.FgCyan, color.Bold).Sprint("jacob"),
	//	color.New(color.Bold).Sprint("@"),
	//	color.New(color.FgGreen, color.Bold).Sprint("replay"),
	//)
	fakePromptUser = color.New(color.FgYellow).Sprint("jacob.work/er-versioning")
)

var (
	skaffoldRunCmd      = newCommand(`skaffold run --profile demo`)
	gitResetWorkflowCmd = newCommand(`git checkout internal/demo/worker/workflow.go`)
	//getWorkerStatusCmd  = newCommand(`kubectl get -o yaml temporalworker sample | yq '.status' | grep -v -E 'apiVersion|resourceVersion|kind|uid|namespace|deployment|name|versionConflictToken' | yq`)
	getWorkerStatusCmd = newCommand(`kubectl get -o yaml temporalworker sample | yq '.status' | grep -v -E 'apiVersion|resourceVersion|kind|uid|namespace|versionConflictToken' | yq`)
)

type demoCommand struct {
	description string
	command     string
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

func (ds demoStep) RunAfterConfirmation(ctx context.Context) error {
	// Print the command before running it
	if len(ds.commands) > 0 {
		printConsole("")
		fmt.Print(commandColor.Sprint(ds.commands[0].command) + " ")
		// wait for ENTER key
		if _, err := fmt.Scanln(); err != nil {
			return fmt.Errorf("error reading input: %w", err)
		}
	}

	return ds.run(ctx, false)
}

func (ds demoStep) run(ctx context.Context, printFirstCommand bool) error {
	//_, _ = faintColor.Printf("# %s\n", ds.description)
	for i, c := range ds.commands {
		if i != 0 || printFirstCommand {
			// Print the command before running it
			if c.description != "" {
				printConsoleComment(c.description + "\n")
			}
			printConsole(commandColor.Sprintf("%s\n", c.command))
		}
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
				//{
				//	description: "Check status of k8s resources: there should only be one deployment",
				//	command:     `kubectl get deployments,pods`,
				//},
			},
		},
		{
			"Describe the temporalworker custom resource",
			[]demoCommand{
				newCommand(`kubectl describe temporalworker sample`),
			},
		},
		{
			"That's a lot of information! Let's just get the status",
			[]demoCommand{
				getWorkerStatusCmd,
			},
		},
		{
			description: "Inspect k8s deployents and pods associated with the worker",
			commands: []demoCommand{
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
				demoCommand{
					description:   "Watch the deployment roll out",
					command:       `kubectl get deployments --watch --output-watch-events`,
					watchDuration: 5 * time.Second,
				},
				newCommand(`kubectl get pods`),
			},
		},
		{
			"Inspect worker status: the deprecated version should still be reachable.",
			[]demoCommand{
				newCommand(`kubectl get -o yaml temporalworker sample | yq '.status' | grep -v -E 'apiVersion|resourceVersion|kind|uid|namespace|deployment|name|versionConflictToken' | yq`),
			},
		},
		{
			"Observe workflows completing on multiple versions",
			[]demoCommand{
				newCommand(`open https://ddstaging.datadoghq.com/dashboard/n7q-tnt-7wt`),
			},
		},
		{
			"Apply progressive rollout strategy",
			[]demoCommand{
				newCommand(`git apply internal/demo/changes/progressive-rollout.patch`),
			},
		},
		{
			"Make another code change",
			//[]demoCommand{
			//	newCommand(`git apply internal/demo/changes/progressive-rollout.patch`),
			//},
			nil,
		},
		{
			"Revert the changes",
			[]demoCommand{
				newCommand(`git reset HEAD~1`),
				gitResetWorkflowCmd,
				newCommand(`git checkout -- internal/demo/temporal_worker.yaml`),
				skaffoldRunCmd,
			},
		},
	}

	runDemo(steps)
}

func runDemo(steps []demoStep) {
	if err := clearConsole(); err != nil {
		log.Fatalf("Error clearing console: %v", err)
	}

	for _, s := range steps {
		// Clear the console
		//if err := clearConsole(); err != nil {
		//	log.Fatalf("Error clearing console: %v", err)
		//}

		// Print the description
		printConsoleComment(s.description + "\n")

		// Run the command
		if err := s.RunAfterConfirmation(context.Background()); err != nil {
			log.Fatalf("Error running command: %v", err)
		}
	}
	printConsoleComment("Demo complete!\n")
}

func clearConsole() error {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

func printConsoleComment(comment string) {
	printConsole(faintColor.Sprint("# " + comment))
}

func printConsole(msg string) {
	fmt.Printf("%s $ %s", fakePromptUser, msg)
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
