package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "go-check",
	Short: "Go code quality checker",
	Run: func(cmd *cobra.Command, args []string) {
		// Default action - run all checks
		runChecks()
	},
}

var fixCmd = &cobra.Command{
	Use:   "fix",
	Short: "Auto-fix issues",
	Run: func(cmd *cobra.Command, args []string) {
		ensureTools()
		runCommand("golangci-lint", "run", "--fix")
		runCommand("gofmt", "-w", ".")
		fmt.Println("✅ Fixed what I could")
	},
}

var deadCmd = &cobra.Command{
	Use:   "dead",
	Short: "Find dead code only",
	Run: func(cmd *cobra.Command, args []string) {
		ensureTools()
		runCommand("deadcode", "./...")
	},
}

func init() {
	rootCmd.AddCommand(fixCmd, deadCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runChecks() {
	ensureTools()
	fmt.Println("🔍 Running checks...")

	failed := false
	if !runCommand("golangci-lint", "run") {
		failed = true
	}
	if !runCommand("deadcode", "./...") {
		failed = true
	}

	if failed {
		fmt.Println("❌ Issues found")
		os.Exit(1)
	}
	fmt.Println("✅ All good")
}

func ensureTools() {
	tools := map[string]string{
		"deadcode":      "golang.org/x/tools/cmd/deadcode@latest",
		"golangci-lint": "github.com/golangci/golangci-lint/cmd/golangci-lint@latest",
	}

	for tool, pkg := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			fmt.Printf("📦 Installing %s...\n", tool)
			cmd := exec.Command("go", "install", pkg)
			cmd.Run()
		}
	}
}

func runCommand(name string, args ...string) bool {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run() == nil
}