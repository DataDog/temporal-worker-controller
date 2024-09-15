package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
)

type demoCommand struct {
	cmd         string
	description string
}

func (c demoCommand) Run() error {
	// Print the description
	fmt.Println(c.description)
	// Run the command
	cmd := exec.Command("sh", "-c", c.cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	commands := []demoCommand{
		{`echo "Hello world"`, "Echo a hello world description"},
		{`echo "Hello REPLAY!!!"`, "Echo a hello replay description"},
	}

	for _, c := range commands {
		// Clear the console
		if err := clearConsole(); err != nil {
			log.Fatalf("Error clearing console: %v", err)
		}

		// Run the command
		if err := c.Run(); err != nil {
			log.Fatalf("Error running command %q: %v", c.cmd, err)
		}

		// wait for ENTER key
		fmt.Println("\nPress ENTER to continue")
		if _, err := fmt.Scanln(); err != nil {
			log.Fatalf("Error reading input: %v", err)
		}
	}
}

func clearConsole() error {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	return cmd.Run()
}
