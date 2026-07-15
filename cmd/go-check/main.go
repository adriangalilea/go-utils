package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/adriangalilea/go-utils" //nolint:staticcheck
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "go-check",
	Short: "Go code quality checker",
	Run: func(cmd *cobra.Command, args []string) {
		runChecks()
	},
}

var fixCmd = &cobra.Command{
	Use:   "fix",
	Short: "Auto-fix issues",
	Run: func(cmd *cobra.Command, args []string) {
		ensureTools()

		fixGoUtilsImports()

		runCommand("golangci-lint", "run", "--fix")
		runCommand("gofmt", "-w", ".")
		Log.Ready("Fixed what I could")
	},
}

var deadCmd = &cobra.Command{
	Use:   "dead",
	Short: "Find dead code (shows all for libraries)",
	Run: func(cmd *cobra.Command, args []string) {
		ensureTools()
		Log.Info("🔍 Running deadcode...")
		Log.Info("Note: In libraries, exported functions will show as 'unreachable'")
		Log.Info("Look for unexported functions - those might be truly dead")
		Log.Info("")
		runCommand("deadcode", "./...")
	},
}

var internalCmd = &cobra.Command{
	Use:   "internal",
	Short: "Check for unused unexported functions",
	Run: func(cmd *cobra.Command, args []string) {
		ensureTools()
		Log.Info("🔍 Checking for unused unexported functions...")
		Log.Info("These are more likely to be truly dead code:")
		Log.Info("")

		deadcodeCmd := exec.Command("deadcode", "./...")
		output, _ := deadcodeCmd.Output()

		lines := strings.Split(string(output), "\n")
		hasDeadCode := false
		for _, line := range lines {
			if line == "" {
				continue
			}
			// Check if it's an unexported function (starts with lowercase after last .)
			parts := strings.Split(line, " ")
			if len(parts) > 2 && strings.Contains(parts[2], "func:") {
				funcName := strings.TrimPrefix(parts[2], "func: ")
				lastDot := strings.LastIndex(funcName, ".")
				if lastDot != -1 && lastDot < len(funcName)-1 {
					firstChar := funcName[lastDot+1]
					if firstChar >= 'a' && firstChar <= 'z' {
						Log.Warn(line)
						hasDeadCode = true
					}
				}
			}
		}

		if !hasDeadCode {
			Log.Ready("✅ No dead unexported functions found")
		} else {
			Log.Error("Found potentially dead unexported functions")
		}
	},
}

func init() {
	rootCmd.AddCommand(fixCmd, deadCmd, internalCmd)
}

func main() {
	Check(rootCmd.Execute())
}

func runChecks() {
	ensureTools()

	// Check for go-utils import issues first
	Log.Info("🔍 Checking go-utils imports...")
	goUtilsIssues := checkGoUtilsImports()
	if goUtilsIssues {
		Log.Info("")
		Log.Info("Tip: Run 'go-check fix' to automatically add //nolint:staticcheck")
	}

	Log.Info("")
	Log.Info("🔍 Running golangci-lint...")
	linterFailed := !runCommand("golangci-lint", "run")

	Log.Info("")
	Log.Info("🔍 Running deadcode...")
	Log.Info("(Note: Public APIs will show as 'unreachable' - this is expected for libraries)")
	deadFailed := !runCommand("deadcode", "./...")

	if goUtilsIssues || linterFailed || deadFailed {
		Log.Error("")
		Log.Error("❌ Issues found")
		os.Exit(1)
	}

	Log.Info("")
	Log.Ready("✅ All checks passed")
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
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			Check(cmd.Run(), "installing", tool) // Fail loud if install fails
		}
	}
}

func runCommand(name string, args ...string) bool {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run() == nil
}

// isDotImportWithoutNolint matches actual import lines only - a real dot
// import starts the line, so string literals mentioning the path (like the
// ones in this file) never match
func isDotImportWithoutNolint(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, `. "github.com/adriangalilea/go-utils"`) &&
		!strings.Contains(line, "//nolint")
}

func checkGoUtilsImports() bool {
	hasIssues := false

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.Contains(path, "vendor/") || strings.Contains(path, ".git/") {
			return nil
		}

		if strings.HasSuffix(path, ".go") {
			file, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer func() { Check(file.Close()) }()

			scanner := bufio.NewScanner(file)
			lineNum := 0
			for scanner.Scan() {
				lineNum++
				if isDotImportWithoutNolint(scanner.Text()) {
					Log.Warn(path + ":" + String(lineNum) + ": dot import of go-utils without //nolint:staticcheck")
					Log.Info("  Fix: Add '//nolint:staticcheck' to the import line")
					hasIssues = true
				}
			}
		}
		return nil
	})

	Check(err)
	return hasIssues
}

func fixGoUtilsImports() {
	fixedCount := 0

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.Contains(path, "vendor/") || strings.Contains(path, ".git/") {
			return nil
		}

		if strings.HasSuffix(path, ".go") {
			lines := strings.Split(string(File.Read(path)), "\n")
			fixed := false
			for i, line := range lines {
				if isDotImportWithoutNolint(line) {
					lines[i] = line + " //nolint:staticcheck"
					fixed = true
				}
			}
			if fixed {
				File.Write(path, []byte(strings.Join(lines, "\n")))
				Log.Info("Fixed:", path)
				fixedCount++
			}
		}
		return nil
	})

	Check(err)

	if fixedCount > 0 {
		Log.Ready("Fixed", fixedCount, "go-utils import(s)")
	}
}
