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
		faint   = color.New(color.Faint)
		fgWhite = color.New(color.FgWhite)
	)

	commands := []demoCommand{
		{`echo "Hello world"`, "Echo a hello world description"},
		{`echo "Hello REPLAY!!!"`, "Echo a hello replay description"},
	}

	for i, c := range commands {
		// Print the description
		//_, _ = color.New(color.Faint).Printf("Next Step: %s\n\n  %s\n", c.description, color.New(color.FgWhite).Sprint(c.cmd))
		fmt.Printf("%s\n\n  %s\n", faint.Sprint("Next step: ", c.description), fgWhite.Sprint(c.cmd))

		// wait for ENTER key
		_, _ = faint.Print("\nPress ENTER to continue ")
		if _, err := fmt.Scanln(); err != nil {
			log.Fatalf("Error reading input: %v", err)
		}

		// Clear the console
		if err := clearConsole(); err != nil {
			log.Fatalf("Error clearing console: %v", err)
		}

		// Run the command
		fmt.Printf("$ %s\n", c.cmd)
		if err := c.Run(); err != nil {
			log.Fatalf("Error running command %q: %v", c.cmd, err)
		}

		if i < len(commands)-1 {
			_, _ = faint.Println("\n---------------------------------------\n")
		}
	}
}

func clearConsole() error {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	return cmd.Run()
}
