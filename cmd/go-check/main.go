package main

import (
	"os"
	"os/exec"

	. "github.com/adriangalilea/go-utils" //nolint:staticcheck
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
		Log.Ready("Fixed what I could")
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
	Check(rootCmd.Execute())
}

func runChecks() {
	ensureTools()
	Log.Info("🔍 Running checks...")

	failed := !runCommand("golangci-lint", "run")
	if !runCommand("deadcode", "./...") {
		failed = true
	}

	if failed {
		Log.Error("Issues found")
		os.Exit(1)
	}
	Log.Ready("All good")
}

func ensureTools() {
	tools := map[string]string{
		"deadcode":      "golang.org/x/tools/cmd/deadcode@latest",
		"golangci-lint": "github.com/golangci/golangci-lint/cmd/golangci-lint@latest",
	}

	for tool, pkg := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			Log.Wait("📦 Installing", tool)
			cmd := exec.Command("go", "install", pkg)
			Must(cmd.Output()) // Fail loud if install fails
		}
	}
}

func runCommand(name string, args ...string) bool {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run() == nil
}