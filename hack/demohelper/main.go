package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/fatih/color"
)

type demoStep struct {
	commands    []string
	description string
}

func (ds demoStep) Run() error {
	for _, c := range ds.commands {
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
	var (
		faintColor   = color.New(color.Faint)
		commandColor = color.New(color.FgHiGreen)
	)

	steps := []demoStep{
		{[]string{`git apply ./internal/demo/changes/version-gate.patch`}, "Switch to workflow.Sleep using version gate"},
		{
			[]string{
				`git checkout internal/demo/worker/workflow.go`,
				`git apply ./internal/demo/changes/no-version-gate.patch`,
			},
			"Remove the version gate",
		},
	}

	for i, s := range steps {
		if i != 0 {
			_, _ = faintColor.Print("\n---------------------------------------------\n")
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
		fmt.Printf("$ %s\n", commandColor.Sprint(s.commands))
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
