package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/fatih/color"
)

type demoCommand struct {
	cmd         string
	description string
}

func (c demoCommand) Run() error {
	cmd := exec.Command("sh", "-c", c.cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	var (
		faintColor   = color.New(color.Faint)
		commandColor = color.New(color.FgHiGreen)
	)

	commands := []demoCommand{
		{`git status`, "Echo a hello world description"},
		{`echo "Hello REPLAY!!!"`, "Echo a hello replay description"},
	}

	for i, c := range commands {
		if i != 0 {
			_, _ = faintColor.Print("\n---------------------------------------------\n")
		}

		// Print the description
		_, _ = faintColor.Print("Next: ", c.description, " [ENTER] ")

		// wait for ENTER key
		if _, err := fmt.Scanln(); err != nil {
			log.Fatalf("Error reading input: %v", err)
		}

		// Clear the console
		if err := clearConsole(); err != nil {
			log.Fatalf("Error clearing console: %v", err)
		}

		// Run the command
		fmt.Printf("$ %s\n", commandColor.Sprint(c.cmd))
		if err := c.Run(); err != nil {
			log.Fatalf("Error running command %q: %v", c.cmd, err)
		}
	}
}

func clearConsole() error {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	return cmd.Run()
}
