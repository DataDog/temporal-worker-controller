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

type demoStep struct {
	description string
	commands    []string
}

func (ds demoStep) Run() error {
	_, _ = faintColor.Printf("# %s\n", ds.description)
	for _, c := range ds.commands {
		fmt.Printf("$ %s\n", commandColor.Sprint(c))
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
		{
			"Deploy v1 of the worker",
			[]string{
				`skaffold run --profile demo`,
			},
		},
		{
			"Switch to workflow.Sleep using a patch/version check",
			[]string{`git apply ./internal/demo/changes/version-gate.patch`},
		},
		{
			"Remove the patch/version check",
			[]string{
				`git checkout internal/demo/worker/workflow.go`,
				`git apply ./internal/demo/changes/no-version-gate.patch`,
			},
		},
		{
			"Deploy the worker",
			[]string{
				`git add internal/demo/worker/workflow.go`,
				`git commit -m "Use workflow.Sleep instead of time.Sleep (no version gate)"`,
				//`git push`,
				`skaffold run --profile demo`,
			},
		},
		{
			"Revert the changes",
			[]string{
				`git reset HEAD~1`,
				`git checkout internal/demo/worker/workflow.go`,
			},
		},
	}

	for i, s := range steps {
		if i != 0 {
			_, _ = faintColor.Print("\n-------------------------\n")
		}

		// Print the description
		_, _ = faintColor.Print("Next: ", s.description, " [ENTER] ")

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
}

func clearConsole() error {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	return cmd.Run()
}
