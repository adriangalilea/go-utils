package utils

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// KEV - A Redis-style KV store for environment variables
//
// ROADMAP: Add transparent memory encryption with ephemeral keys
//   - All in-memory values encrypted at rest
//   - Ephemeral key generated at startup, never persisted
//   - Zero-copy decryption on Get() operations
//   - Enables safe integration with secret vaults (HashiCorp, AWS Secrets Manager, etc)
//   - Memory dumps/core files won't leak secrets
//
// TODO: .kenv file format (encrypted env file)
//   - Type-safe schema definition (string, int, bool, duration, etc)
//   - AES-256-GCM encryption with password derivation (Argon2)
//   - Single file "vault" for secrets
//   - Not safe for git, but safe for scp/transmission
//
// TODO: kev CLI tool
//   kev init secrets.kenv              # Create new encrypted file
//   kev set secrets.kenv API_KEY value  # Add/update key
//   kev get secrets.kenv API_KEY        # Retrieve value
//   kev list secrets.kenv               # List all keys (not values)
//   kev export secrets.kenv > .env      # Decrypt to .env format
//   kev import .env > secrets.kenv      # Encrypt from .env
//   kev serve secrets.kenv              # HTTP server for secrets (local only)
//
// DEFAULT USAGE (no namespaces needed!):
//   apiKey := KEV.MustGet("API_KEY")      // Panics if not found (required config)
//   apiKey := KEV.Get("API_KEY")          // Returns "" if not found
//   apiKey := KEV.Get("API_KEY", "dev")   // Returns "dev" if not found
//   port := KEV.Int("PORT", 8080)         // With type conversion
//   KEV.Set("DEBUG", "true")              // Sets in memory (fast)
//
//   KEV.Get("DATABASE_URL")               // memory → os → .env → cache result
//   KEV.Get("DATABASE_URL")               // memory (cached!) ✓
//
// CUSTOMIZE THE SEARCH ORDER:
//   KEV.Source.Remove("os")                // Ignore OS env (perfect for tests!)
//   KEV.Source.Add(".env.local")           // Add more fallbacks
//   KEV.Source.Set(".env.test")            // Or replace entirely
//
// REDIS-STYLE NAMESPACING (when you need control):
//   KEV.Get("os:PATH")                     // ONLY from OS, no fallback
//   KEV.Get(".env:API_KEY")                // ONLY from .env file
//   KEV.Set("os:DEBUG", "true")            // Write directly to OS
//   KEV.Set(".env:API_KEY", "secret")      // Update .env file
//
//   // Pattern matching
//   KEV.Keys("API_*")                      // Find all API_ keys
//   KEV.All("os:*")                        // Get all OS vars
//   KEV.Clear("TEMP_*")                    // Clean up temp vars
//
// SOURCE TRACKING & OBSERVABILITY:
//   value, source := KEV.GetWithSource("API_KEY")  // Returns value + where it came from
//   source := KEV.SourceOf("API_KEY")              // "/path/to/project/.env"
//   KEV.Debug = true                                // Shows lookup chain
//   KEV.Export("backup.env")                        // Includes # from: comments
//
// AUTO PROJECT ROOT DISCOVERY:
//   // Automatically finds project root (go.mod, .git) and adds projectRoot/.env
//   // No more ../../../.env guessing!
//   // From any nested directory in your project, KEV finds the root .env
//   // Local .env still takes precedence for overrides
//
// TURBOREPO MONOREPO SUPPORT:
//   // If in a turborepo monorepo, automatically adds monorepoRoot/.env
//   // Priority: local .env → monorepoRoot/.env → projectRoot/.env → os
//   // Perfect for shared config across monorepo packages
//
// WHY THIS IS POWERFUL:
//   - Zero config: Works immediately with sensible defaults
//   - Project aware: Auto-discovers project root .env (no more ../.. hell)
//   - Test isolation: Remove "os" source for hermetic tests
//   - Debug easily: KEV.All("*:*") shows everything, everywhere
//   - Full observability: Always know where your config came from
//   - Redis familiar: If you know Redis, you know KEV

type memEntry struct {
	value  string
	source string // "os", ".env", "../.env", "default", "set", etc.
}

type kevOps struct {
	mu      sync.RWMutex
	memory  map[string]memEntry
	sources []string  // Default search order for unnamespaced keys
	Source  sourceOps // Source management operations
	Debug   bool      // Enable debug logging
}

var KEV = &kevOps{
	memory:  make(map[string]memEntry),
	sources: []string{"os", ".env"}, // Default sources
	Debug:   false,
}

// Auto-discover monorepo/project root .env files as fallback sources
func init() {
	KEV.Source = sourceOps{kev: KEV}

	// Check for monorepo root first (turborepo)
	monorepoRoot := findMonorepoRoot()
	if monorepoRoot != "" {
		// Add monorepo root .env with highest priority (after local .env)
		monorepoEnv := filepath.Join(monorepoRoot, ".env")
		KEV.sources = append(KEV.sources, monorepoEnv)

		if KEV.Debug {
			Log.Info("KEV", "Auto-discovered monorepo root:", monorepoRoot)
			Log.Info("KEV", "Added monorepo .env to sources:", monorepoEnv)
		}
	}

	// Then check for project root
	projectRoot := findProjectRoot()
	if projectRoot != "" {
		projectEnv := filepath.Join(projectRoot, ".env")
		// Only add if it's different from monorepo env
		if monorepoRoot == "" || projectEnv != filepath.Join(monorepoRoot, ".env") {
			KEV.sources = append(KEV.sources, projectEnv)

			if KEV.Debug {
				Log.Info("KEV", "Auto-discovered project root:", projectRoot)
				Log.Info("KEV", "Added project .env to sources:", projectEnv)
			}
		}
	}

	if KEV.Debug {
		if monorepoRoot == "" && projectRoot == "" {
			Log.Info("KEV", "No project or monorepo root found, using standard sources:", KEV.sources)
		} else {
			Log.Info("KEV", "Default sources:", KEV.sources)
		}
	}
}

// parseKey splits "namespace:key" or returns ("", key) for unnamespaced
func parseKey(key string) (namespace, k string) {
	if strings.HasPrefix(key, ":") {
		panic(&Panic{Message: String("invalid key format - starts with colon:", key)})
	}
	if strings.Contains(key, "::") {
		panic(&Panic{Message: String("invalid key format - double colon:", key)})
	}

	parts := strings.SplitN(key, ":", 2)
	if len(parts) == 2 {
		if parts[0] == "" || parts[1] == "" {
			panic(&Panic{Message: String("invalid key format - empty namespace or key:", key)})
		}
		return parts[0], parts[1]
	}
	return "", key
}

// Get returns environment variable with optional default.
//
// Namespaced keys (direct access, no memory):
//
//	KEV.Get("os:PATH")         - Direct from OS env
//	KEV.Get(".env:API_KEY")    - Direct from .env file
//
// Unnamespaced keys (memory + source fallback):
//
//	KEV.Get("API_KEY")         - Check memory, then sources
//	KEV.Get("PORT", "8080")    - With default value
func (k *kevOps) Get(key string, defaultValue ...string) string {
	namespace, realKey := parseKey(key)

	// Centralized debug flag - log level lookups themselves must never be
	// debug-logged or every log line would recurse through here
	debug := k.Debug && !strings.HasSuffix(key, "LOG_LEVEL")

	if debug {
		Log.Info("KEV", "Looking for", key)
	}

	if namespace != "" {
		val := k.getFromNamespace(namespace, realKey)
		if val != "" {
			if debug {
				Log.Info("KEV", "  ✓", namespace+":", "found", val)
			}
			return val
		}
		if debug {
			Log.Info("KEV", "  ✗", namespace+":", "not found")
		}
		if len(defaultValue) > 0 {
			if debug {
				Log.Info("KEV", "  → using default:", defaultValue[0])
			}
			return defaultValue[0]
		}
		return ""
	}

	k.mu.RLock()
	entry, exists := k.memory[realKey]
	sources := k.sources
	k.mu.RUnlock()

	if exists {
		if debug {
			Log.Info("KEV", "  ✓ memory:", entry.value, "(from", entry.source+")")
		}
		return entry.value
	}

	if debug {
		Log.Info("KEV", "  ✗ memory: not found")
	}

	for _, source := range sources {
		if val := k.getFromNamespace(source, realKey); val != "" {
			if debug {
				Log.Info("KEV", "  ✓", source+":", "found", val, "(caching)")
			}
			// Store absolute path for file sources
			absoluteSource := source
			if source != "os" && source != "default" && source != "set" {
				if absPath, err := filepath.Abs(source); err == nil {
					absoluteSource = absPath
				}
			}
			k.mu.Lock()
			k.memory[realKey] = memEntry{value: val, source: absoluteSource}
			k.mu.Unlock()
			return val
		}
		if debug {
			Log.Info("KEV", "  ✗", source+":", "not found")
		}
	}

	if len(defaultValue) > 0 {
		if debug {
			Log.Info("KEV", "  → using default:", defaultValue[0], "(caching)")
		}
		k.mu.Lock()
		k.memory[realKey] = memEntry{value: defaultValue[0], source: "default"}
		k.mu.Unlock()
		return defaultValue[0]
	}

	if debug {
		Log.Info("KEV", "  → not found, returning empty")
	}
	return ""
}

// MustGet returns environment variable or panics if not found.
// Uses Must() internally - fail loud on missing required config.
//
// Examples:
//
//	apiKey := KEV.MustGet("API_KEY")      // Panics if not found
//	dbUrl := KEV.MustGet("DATABASE_URL")  // Required config
//	path := KEV.MustGet("os:PATH")        // Must exist in OS
func (k *kevOps) MustGet(key string) string {
	val := k.Get(key)
	if val == "" {
		panic(&Panic{Message: String("required key not found:", key)})
	}
	return val
}

// SourceOf returns where a cached key came from.
// Returns empty string if key is not in cache.
//
// Examples:
//
//	source := KEV.SourceOf("API_KEY")  // ".env" or "os" or "default"
//	if source == "" { /* not cached */ }
func (k *kevOps) SourceOf(key string) string {
	k.mu.RLock()
	defer k.mu.RUnlock()

	if entry, exists := k.memory[key]; exists {
		return entry.source
	}
	return ""
}

// GetWithSource returns both the value and its source.
// Source will be empty if value is not found.
//
// Examples:
//
//	value, source := KEV.GetWithSource("API_KEY")
//	fmt.Printf("API_KEY = %s (from %s)\n", value, source)
func (k *kevOps) GetWithSource(key string, defaultValue ...string) (value, source string) {
	value = k.Get(key, defaultValue...)
	if value != "" {
		source = k.SourceOf(key)
		// If not in cache but has value, it might be a namespaced get
		if source == "" {
			namespace, _ := parseKey(key)
			if namespace != "" {
				source = namespace
			}
		}
	}
	return value, source
}

// isFileNamespace reports whether a namespace refers to an env file
// (.env, .env.local, ../.env, /abs/path.env, ...)
func isFileNamespace(namespace string) bool {
	return strings.HasPrefix(namespace, ".") || strings.Contains(namespace, "/")
}

// assertKnownNamespace screams on namespaces KEV doesn't understand -
// a silent no-op on a typo'd namespace is how config bugs hide.
func assertKnownNamespace(namespace string) {
	Assert(namespace == "os" || isFileNamespace(namespace),
		"unknown KEV namespace:", namespace, "(want \"os\" or a file path like \".env\")")
}

func (k *kevOps) getFromNamespace(namespace, key string) string {
	assertKnownNamespace(namespace)
	if namespace == "os" {
		return os.Getenv(key)
	}
	return k.getFromFile(namespace, key)
}

// Set sets environment variable.
//
// Namespaced (direct write):
//
//	KEV.Set("os:DEBUG", "true")       - Sets in OS env
//	KEV.Set(".env:API_KEY", "secret") - Writes to .env file
//
// Unnamespaced (memory only):
//
//	KEV.Set("API_KEY", "secret")      - Sets in memory only
func (k *kevOps) Set(key, value string) {
	namespace, realKey := parseKey(key)

	// Namespaced - direct write
	if namespace != "" {
		k.setToNamespace(namespace, realKey, value)
		return
	}

	// Unnamespaced - memory only
	k.mu.Lock()
	k.memory[realKey] = memEntry{value: value, source: "set"}
	k.mu.Unlock()
}

func (k *kevOps) setToNamespace(namespace, key, value string) {
	assertKnownNamespace(namespace)
	if namespace == "os" {
		Check(os.Setenv(key, value))
		return
	}
	k.setToFile(namespace, key, value)
}

func (k *kevOps) Has(key string) bool {
	namespace, realKey := parseKey(key)

	if namespace != "" {
		return k.hasInNamespace(namespace, realKey)
	}

	k.mu.RLock()
	_, exists := k.memory[realKey]
	sources := k.sources
	k.mu.RUnlock()

	if exists {
		return true
	}

	for _, source := range sources {
		if k.hasInNamespace(source, realKey) {
			return true
		}
	}

	return false
}

// hasInNamespace checks if key exists in namespace, even with an empty value
func (k *kevOps) hasInNamespace(namespace, key string) bool {
	assertKnownNamespace(namespace)
	if namespace == "os" {
		_, exists := os.LookupEnv(key)
		return exists
	}
	matches := make(map[string]string)
	k.parseEnvFile(namespace, key, matches, true)
	_, exists := matches[key]
	return exists
}

// Keys returns all keys, optionally filtered by pattern
// Supports namespaced patterns: "os:API_*", ".env:*"
func (k *kevOps) Keys(patterns ...string) []string {
	var keys []string
	seen := make(map[string]bool)

	if len(patterns) == 0 {
		patterns = []string{"*"}
	}

	for _, pattern := range patterns {
		namespace, keyPattern := parseKey(pattern)

		if namespace != "" {
			nsKeys := k.keysFromNamespace(namespace, keyPattern)
			for _, key := range nsKeys {
				fullKey := namespace + ":" + key
				if !seen[fullKey] {
					keys = append(keys, fullKey)
					seen[fullKey] = true
				}
			}
		} else {
			k.mu.RLock()
			for key := range k.memory {
				if matchPattern(key, keyPattern) && !seen[key] {
					keys = append(keys, key)
					seen[key] = true
				}
			}
			sources := k.sources
			k.mu.RUnlock()

			for _, source := range sources {
				nsKeys := k.keysFromNamespace(source, keyPattern)
				for _, key := range nsKeys {
					if !seen[key] {
						keys = append(keys, key)
						seen[key] = true
					}
				}
			}
		}
	}

	return keys
}

// getNamespaceData retrieves data from a namespace with optional pattern matching
// If keysOnly is true, returns keys as values (for backwards compat with keysFromNamespace)
func (k *kevOps) getNamespaceData(namespace, pattern string, keysOnly bool) map[string]string {
	result := make(map[string]string)

	switch namespace {
	case "os":
		environ := os.Environ()
		for _, env := range environ {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) >= 1 {
				key := parts[0]
				if matchPattern(key, pattern) {
					if keysOnly {
						result[key] = "" // Keys only mode
					} else if len(parts) == 2 {
						result[key] = parts[1] // Full key-value mode
					}
				}
			}
		}
	default:
		assertKnownNamespace(namespace)
		k.parseEnvFile(namespace, pattern, result, keysOnly)
	}

	return result
}

// parseEnvFile reads and parses an env file, extracting matching keys/values.
// The single parser every file-namespace read goes through.
func (k *kevOps) parseEnvFile(path, pattern string, result map[string]string, keysOnly bool) {
	if !File.Exists(path) {
		return
	}

	file := File.Open(path)
	defer func() { Check(file.Close()) }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) >= 1 {
			key := strings.TrimSpace(parts[0])
			if matchPattern(key, pattern) {
				if keysOnly {
					result[key] = "" // Keys only mode
				} else if len(parts) == 2 {
					value := strings.TrimSpace(parts[1])
					value = strings.Trim(value, `"'`)
					result[key] = value // Full key-value mode
				}
			}
		}
	}
}

func (k *kevOps) getAllFromNamespace(namespace, pattern string) map[string]string {
	return k.getNamespaceData(namespace, pattern, false)
}

func (k *kevOps) keysFromNamespace(namespace, pattern string) []string {
	data := k.getNamespaceData(namespace, pattern, true)
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	return keys
}

// All returns all variables matching patterns.
//
// Always returns map[string]map[string]string organized by namespace:
//
//	KEV.All()         // {"memory": {...}} - just memory
//	KEV.All("API_*")  // {"memory": {...}, "os": {...}, ".env": {...}} - filtered
//	KEV.All("os:*")   // {"os": {...}} - just os namespace
//	KEV.All("*:*")    // {"memory": {...}, "os": {...}, ".env": {...}} - everything
//
// Examples:
//
//	// Debug what's loaded
//	fmt.Println(KEV.All())
//
//	// Compare across namespaces
//	all := KEV.All("*:*")
//	if all["os"]["DEBUG"] != all[".env"]["DEBUG"] {
//	    fmt.Println("Environment mismatch!")
//	}
func (k *kevOps) All(patterns ...string) map[string]map[string]string {
	result := make(map[string]map[string]string)

	if len(patterns) == 0 {
		k.mu.RLock()
		if len(k.memory) > 0 {
			memCopy := make(map[string]string)
			for key, entry := range k.memory {
				memCopy[key] = entry.value + " [from: " + entry.source + "]"
			}
			result["memory"] = memCopy
		}
		k.mu.RUnlock()
		return result
	}

	hasNamespace := false
	for _, pattern := range patterns {
		if strings.Contains(pattern, ":") {
			hasNamespace = true
			break
		}
	}

	if !hasNamespace {
		k.mu.RLock()
		memMatches := make(map[string]string)
		for key, entry := range k.memory {
			for _, pattern := range patterns {
				if matchPattern(key, pattern) {
					memMatches[key] = entry.value + " [from: " + entry.source + "]"
					break
				}
			}
		}
		if len(memMatches) > 0 {
			result["memory"] = memMatches
		}
		sources := k.sources
		k.mu.RUnlock()

		for _, source := range sources {
			sourceMatches := make(map[string]string)
			sourceVars := k.getAllFromNamespace(source, "*")
			for key, val := range sourceVars {
				for _, pattern := range patterns {
					if matchPattern(key, pattern) {
						sourceMatches[key] = val
						break
					}
				}
			}
			if len(sourceMatches) > 0 {
				result[source] = sourceMatches
			}
		}
		return result
	}

	if len(patterns) == 1 && patterns[0] == "*:*" {
		k.mu.RLock()
		if len(k.memory) > 0 {
			memCopy := make(map[string]string)
			for key, entry := range k.memory {
				memCopy[key] = entry.value + " [from: " + entry.source + "]"
			}
			result["memory"] = memCopy
		}
		k.mu.RUnlock()

		k.mu.RLock()
		sources := k.sources
		k.mu.RUnlock()
		for _, source := range sources {
			sourceVars := k.getAllFromNamespace(source, "*")
			if len(sourceVars) > 0 {
				result[source] = sourceVars
			}
		}
	} else {
		for _, pattern := range patterns {
			namespace, keyPattern := parseKey(pattern)
			if namespace != "" {
				nsVars := k.getAllFromNamespace(namespace, keyPattern)
				if len(nsVars) > 0 {
					if result[namespace] == nil {
						result[namespace] = make(map[string]string)
					}
					for key, val := range nsVars {
						result[namespace][key] = val
					}
				}
			}
		}
	}

	return result
}

// Clear removes variables from memory only.
// For namespaced clearing, use ClearUnsafe.
//
// Examples:
//
//	KEV.Clear()           // Clear all memory
//	KEV.Clear("API_*")    // Clear API_* from memory only
func (k *kevOps) Clear(patterns ...string) {
	for _, pattern := range patterns {
		if strings.Contains(pattern, ":") {
			panic(&Panic{Message: String("Clear() with namespace is dangerous! Use ClearUnsafe() if you really need this.")})
		}
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	if len(patterns) == 0 {
		k.memory = make(map[string]memEntry)
		return
	}

	for _, pattern := range patterns {
		for key := range k.memory {
			if matchPattern(key, pattern) {
				delete(k.memory, key)
			}
		}
	}
}

// ClearUnsafe allows clearing from namespaces (OS, files).
// Required for operations that modify OS env or files.
//
// Examples:
//
//	KEV.ClearUnsafe("os:TEMP_*")     // Remove TEMP_* from OS
//	KEV.ClearUnsafe("os:DEBUG")      // Remove specific OS var
func (k *kevOps) ClearUnsafe(patterns ...string) {
	for _, pattern := range patterns {
		namespace, keyPattern := parseKey(pattern)

		if namespace == "" {
			k.Clear(pattern)
			continue
		}

		switch namespace {
		case "os":
			if keyPattern == "*" {
				panic(&Panic{Message: String("ClearUnsafe(\"os:*\") would destroy system! This is never allowed.")})
			}
			keys := k.keysFromNamespace("os", keyPattern)
			for _, key := range keys {
				Check(os.Unsetenv(key))
			}
		default:
			panic(&Panic{Message: String("Clearing from files not yet implemented")})
		}
	}
}

// Unset removes specific keys
func (k *kevOps) Unset(keys ...string) {
	for _, key := range keys {
		namespace, realKey := parseKey(key)

		if namespace != "" {
			switch namespace {
			case "os":
				Check(os.Unsetenv(realKey))
			default:
				panic(&Panic{Message: String("Unsetting from files not yet implemented")})
			}
		} else {
			k.mu.Lock()
			delete(k.memory, realKey)
			k.mu.Unlock()
		}
	}
}

// Int returns environment variable as int with default.
// Panics on invalid format (offensive programming - fail loud on bad data).
//
// Example:
//
//	port := KEV.Int("PORT", 8080)
//	workers := KEV.Int("os:WORKERS", 4)
func (k *kevOps) Int(key string, defaultValue int) int {
	val := k.Get(key)
	if val == "" {
		return defaultValue
	}

	i, err := strconv.Atoi(val)
	if err != nil {
		panic(&Panic{Message: String("invalid int value for", key, ":", val)})
	}
	return i
}

// Bool returns environment variable as bool with default
// Recognizes: true/false, 1/0, yes/no, on/off
func (k *kevOps) Bool(key string, defaultValue bool) bool {
	val := strings.ToLower(k.Get(key))
	if val == "" {
		return defaultValue
	}

	switch val {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		panic(&Panic{Message: String("invalid bool value for", key, ":", val)})
	}
}

// Float returns environment variable as float64 with default.
// Panics on invalid format (offensive programming - fail loud on bad data).
//
// Example:
//
//	threshold := KEV.Float("ALERT_THRESHOLD", 0.95)
func (k *kevOps) Float(key string, defaultValue float64) float64 {
	val := k.Get(key)
	if val == "" {
		return defaultValue
	}

	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		panic(&Panic{Message: String("invalid float value for", key, ":", val)})
	}
	return f
}

// Duration returns environment variable as time.Duration with default
func (k *kevOps) Duration(key string, defaultValue time.Duration) time.Duration {
	val := k.Get(key)
	if val == "" {
		return defaultValue
	}

	d, err := time.ParseDuration(val)
	if err != nil {
		panic(&Panic{Message: String("invalid duration value for", key, ":", val)})
	}
	return d
}

// Export saves all memory variables to a file.
// Source info goes on its own comment line - inline comments would be
// read back as part of the value by any env parser, including KEV's own.
func (k *kevOps) Export(path string) {
	k.mu.RLock()
	var lines []string
	for key, entry := range k.memory {
		val := entry.value
		// Escape values with spaces or special chars
		if strings.ContainsAny(val, " \t\n\"'") {
			val = fmt.Sprintf("%q", val)
		}
		lines = append(lines, "# from: "+entry.source, key+"="+val)
	}
	k.mu.RUnlock()

	content := strings.Join(lines, "\n") + "\n"
	File.Write(path, []byte(content))
}

// Dump prints all environment variables (masks sensitive keys)
func (k *kevOps) Dump() {
	all := k.All()

	for namespace, vars := range all {
		for key, val := range vars {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "key") ||
				strings.Contains(lower, "secret") ||
				strings.Contains(lower, "password") ||
				strings.Contains(lower, "token") {
				if len(val) > 4 {
					val = val[:4] + "****"
				} else {
					val = "****"
				}
			}
			fmt.Println(namespace + ":" + key + "=" + val)
		}
	}
}

// sourceOps manages the fallback chain
//
// The source list determines fallback order for unnamespaced Gets.
// Default: ["os", ".env"]
//
// Examples:
//
//	// Remove OS for testing
//	KEV.Source.Remove("os")
//	KEV.Get("PATH")  // Now only checks memory → .env
//
//	// Add more sources
//	KEV.Source.Add(".env.local", ".env.production")
//	KEV.Get("API_KEY")  // Checks: memory → os → .env → .env.local → .env.production
//
//	// Replace entirely
//	KEV.Source.Set(".env.test")
//	KEV.Get("API_KEY")  // Only checks memory → .env.test
type sourceOps struct {
	kev *kevOps
}

func (s sourceOps) Set(sources ...string) {
	s.kev.mu.Lock()
	defer s.kev.mu.Unlock()
	s.kev.sources = sources
}

func (s sourceOps) Add(sources ...string) {
	s.kev.mu.Lock()
	defer s.kev.mu.Unlock()
	s.kev.sources = append(s.kev.sources, sources...)
}

func (s sourceOps) Remove(sources ...string) {
	s.kev.mu.Lock()
	defer s.kev.mu.Unlock()

	newSources := []string{}
	for _, existing := range s.kev.sources {
		remove := false
		for _, toRemove := range sources {
			if existing == toRemove {
				remove = true
				break
			}
		}
		if !remove {
			newSources = append(newSources, existing)
		}
	}
	s.kev.sources = newSources
}

func (s sourceOps) List() []string {
	s.kev.mu.RLock()
	defer s.kev.mu.RUnlock()

	result := make([]string, len(s.kev.sources))
	copy(result, s.kev.sources)
	return result
}

func (s sourceOps) Clear() {
	s.kev.mu.Lock()
	defer s.kev.mu.Unlock()
	s.kev.sources = []string{}
}

// getFromFile reads a specific key from a file (last occurrence wins)
func (k *kevOps) getFromFile(path string, key string) string {
	matches := make(map[string]string)
	k.parseEnvFile(path, key, matches, false)
	return matches[key]
}

func (k *kevOps) setToFile(path string, key string, value string) {
	var lines []string
	found := false

	if File.Exists(path) {
		content := File.Read(path)
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)

			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				lines = append(lines, line)
				continue
			}

			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) >= 1 {
				fileKey := strings.TrimSpace(parts[0])
				if fileKey == key {
					if strings.ContainsAny(value, " \t\n\"'") {
						value = fmt.Sprintf("%q", value)
					}
					lines = append(lines, key+"="+value)
					found = true
				} else {
					lines = append(lines, line)
				}
			} else {
				lines = append(lines, line)
			}
		}
	}

	if !found {
		if strings.ContainsAny(value, " \t\n\"'") {
			value = fmt.Sprintf("%q", value)
		}
		lines = append(lines, key+"="+value)
	}

	content := strings.Join(lines, "\n")
	File.Write(path, []byte(content))
}

// matchPattern matches a key against a glob pattern (*, ?, [...]).
// Panics on malformed patterns - a bad pattern is a bug, not a miss.
func matchPattern(key, pattern string) bool {
	matched, err := path.Match(pattern, key)
	Check(err, "invalid KEV pattern:", pattern)
	return matched
}
