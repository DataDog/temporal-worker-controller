package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/fatih/color"
)

var (
	faintColor   = color.New(color.Faint)
	commandColor = color.New(color.FgHiGreen)
)

const (
	skaffoldRunCmd      = `skaffold run --profile demo`
	gitResetWorkflowCmd = `git checkout internal/demo/worker/workflow.go`
)

type demoStep struct {
	description string
	commands    []string
}

func (ds demoStep) Run() error {
	_, _ = faintColor.Printf("# %s\n", ds.description)
	for _, c := range ds.commands {
		// Print the command before running it
		fmt.Printf("$ %s\n", commandColor.Sprint(c))
		// Run the command
		cmd := exec.Command("sh", "-c", c)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	steps := []demoStep{
		//{
		//	"Deploy v1 of the worker",
		//	[]string{
		//		`skaffold run --profile demo`,
		//	},
		//},
		//{
		//	"Switch to workflow.Sleep using a patch/version check",
		//	[]string{`git apply ./internal/demo/changes/version-gate.patch`},
		//},
		//{
		//	"Remove the patch/version check",
		//	[]string{
		//		gitResetWorkflowCmd,
		//		`git apply ./internal/demo/changes/no-version-gate.patch`,
		//	},
		//},
		//{
		//	"Deploy the worker",
		//	[]string{
		//		`git add internal/demo/worker/workflow.go`,
		//		`git commit -m "Use workflow.Sleep instead of time.Sleep (no version gate)"`,
		//		//`git push`,
		//		skaffoldRunCmd,
		//	},
		//},
		{
			"Inspect worker state by describing status: the deprecated worker version is still reachable.",
			[]string{
				`kubectl describe temporalworker sample`,
			},
		},
		//{
		//	"Revert the changes",
		//	[]string{
		//		`git reset HEAD~1`,
		//		gitResetWorkflowCmd,
		//	},
		//},
		//{
		//	"Deploy v2 of the worker",
		//	[]string{
		//		skaffoldRunCmd,
		//	},
		//},
	}

	for _, s := range steps {
		// Print the description
		_, _ = faintColor.Print("# Next: ", s.description, " [ENTER] ")

		// wait for ENTER key
		if _, err := fmt.Scanln(); err != nil {
			log.Fatalf("Error reading input: %v", err)
		}

		// Clear the console
		if err := clearConsole(); err != nil {
			log.Fatalf("Error clearing console: %v", err)
		}

		// Run the command
		if err := s.Run(); err != nil {
			log.Fatalf("Error running command %q: %v", s.commands, err)
		}
	}

	_, _ = faintColor.Println("# Demo complete!")
}

func clearConsole() error {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	return cmd.Run()
}
