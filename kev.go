package utils

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// KEV v0.0.2 - A Redis-style KV store for environment variables
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
// KEV - A Redis-style KV store for environment variables
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
// WHY THIS IS POWERFUL:
//   - Zero config: Works immediately with sensible defaults
//   - Test isolation: Remove "os" source for hermetic tests  
//   - Debug easily: KEV.All("*:*") shows everything, everywhere
//   - Redis familiar: If you know Redis, you know KEV

type kevOps struct {
	mu      sync.RWMutex
	memory  map[string]string
	sources []string // Default search order for unnamespaced keys
	Source  sourceOps // Source management operations
}

// kev provides environment variable operations with KV store semantics
var kev = &kevOps{
	memory:  make(map[string]string),
	sources: []string{"os", ".env"}, // Default sources
}

// KEV is the public interface for environment operations
var KEV = kev

// Initialize Source directly - no init() magic
var _ = func() bool {
	KEV.Source = sourceOps{kev: KEV}
	return true
}()

// parseKey splits "namespace:key" or returns ("", key) for unnamespaced
func parseKey(key string) (namespace, k string) {
	// Check for invalid patterns
	if strings.HasPrefix(key, ":") {
		panic(Error("invalid key format - starts with colon:", key))
	}
	if strings.Contains(key, "::") {
		panic(Error("invalid key format - double colon:", key))
	}
	
	parts := strings.SplitN(key, ":", 2)
	if len(parts) == 2 {
		// Validate namespace and key aren't empty
		if parts[0] == "" || parts[1] == "" {
			panic(Error("invalid key format - empty namespace or key:", key))
		}
		return parts[0], parts[1]
	}
	return "", key
}

// Get returns environment variable with optional default.
//
// Namespaced keys (direct access, no memory):
//   KEV.Get("os:PATH")         - Direct from OS env
//   KEV.Get(".env:API_KEY")    - Direct from .env file
//
// Unnamespaced keys (memory + source fallback):
//   KEV.Get("API_KEY")         - Check memory, then sources
//   KEV.Get("PORT", "8080")    - With default value
func (k *kevOps) Get(key string, defaultValue ...string) string {
	namespace, realKey := parseKey(key)
	
	// Namespaced - direct access
	if namespace != "" {
		val := k.getFromNamespace(namespace, realKey)
		if val != "" {
			return val
		}
		// Use default if provided
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return ""
	}
	
	// Unnamespaced - check memory first
	k.mu.RLock()
	val, exists := k.memory[realKey]
	sources := k.sources
	k.mu.RUnlock()
	
	if exists {
		return val
	}
	
	// Search through sources
	for _, source := range sources {
		if val := k.getFromNamespace(source, realKey); val != "" {
			// Cache in memory for next time
			k.mu.Lock()
			k.memory[realKey] = val
			k.mu.Unlock()
			return val
		}
	}
	
	// Use default and cache it
	if len(defaultValue) > 0 {
		k.mu.Lock()
		k.memory[realKey] = defaultValue[0]
		k.mu.Unlock()
		return defaultValue[0]
	}
	
	return ""
}

// MustGet returns environment variable or panics if not found.
// Uses Must() internally - fail loud on missing required config.
//
// Examples:
//   apiKey := KEV.MustGet("API_KEY")      // Panics if not found
//   dbUrl := KEV.MustGet("DATABASE_URL")  // Required config
//   path := KEV.MustGet("os:PATH")        // Must exist in OS
func (k *kevOps) MustGet(key string) string {
	val := k.Get(key)
	if val == "" {
		panic(Error("required key not found:", key))
	}
	return val
}

// getFromNamespace gets a value directly from a namespace
func (k *kevOps) getFromNamespace(namespace, key string) string {
	switch namespace {
	case "os":
		return os.Getenv(key)
	default:
		// File namespace (.env, .env.local, etc)
		if strings.HasPrefix(namespace, ".") || strings.Contains(namespace, "/") {
			return k.getFromFile(namespace, key)
		}
	}
	return ""
}

// Set sets environment variable.
//
// Namespaced (direct write):
//   KEV.Set("os:DEBUG", "true")       - Sets in OS env
//   KEV.Set(".env:API_KEY", "secret") - Writes to .env file
//
// Unnamespaced (memory only):
//   KEV.Set("API_KEY", "secret")      - Sets in memory only
func (k *kevOps) Set(key, value string) {
	namespace, realKey := parseKey(key)
	
	// Namespaced - direct write
	if namespace != "" {
		k.setToNamespace(namespace, realKey, value)
		return
	}
	
	// Unnamespaced - memory only
	k.mu.Lock()
	k.memory[realKey] = value
	k.mu.Unlock()
}

// setToNamespace sets a value directly to a namespace
func (k *kevOps) setToNamespace(namespace, key, value string) {
	switch namespace {
	case "os":
		err := os.Setenv(key, value)
		Check(err)
	default:
		// File namespace - update or append to file
		if strings.HasPrefix(namespace, ".") || strings.Contains(namespace, "/") {
			k.setToFile(namespace, key, value)
		}
	}
}

// Has checks if key exists
func (k *kevOps) Has(key string) bool {
	namespace, realKey := parseKey(key)
	
	// Namespaced - check directly
	if namespace != "" {
		return k.hasInNamespace(namespace, realKey)
	}
	
	// Unnamespaced - check memory then sources
	k.mu.RLock()
	_, exists := k.memory[realKey]
	sources := k.sources
	k.mu.RUnlock()
	
	if exists {
		return true
	}
	
	// Check sources
	for _, source := range sources {
		if k.hasInNamespace(source, realKey) {
			return true
		}
	}
	
	return false
}

// hasInNamespace checks if key exists in namespace without reading full value
func (k *kevOps) hasInNamespace(namespace, key string) bool {
	switch namespace {
	case "os":
		_, exists := os.LookupEnv(key)
		return exists
	default:
		// File namespace - still need to read file but stop early
		if strings.HasPrefix(namespace, ".") || strings.Contains(namespace, "/") {
			if !File.Exists(namespace) {
				return false
			}
			file, err := os.Open(namespace)
			if err != nil {
				return false
			}
			defer file.Close()
			
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				
				parts := strings.SplitN(line, "=", 2)
				if len(parts) >= 1 {
					fileKey := strings.TrimSpace(parts[0])
					if fileKey == key {
						return true
					}
				}
			}
		}
	}
	return false
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
			// Namespaced pattern
			nsKeys := k.keysFromNamespace(namespace, keyPattern)
			for _, key := range nsKeys {
				fullKey := namespace + ":" + key
				if !seen[fullKey] {
					keys = append(keys, fullKey)
					seen[fullKey] = true
				}
			}
		} else {
			// Unnamespaced - get from memory and sources
			k.mu.RLock()
			for key := range k.memory {
				if matchPattern(key, keyPattern) && !seen[key] {
					keys = append(keys, key)
					seen[key] = true
				}
			}
			sources := k.sources
			k.mu.RUnlock()
			
			// Also check sources
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
						result[key] = ""  // Keys only mode
					} else if len(parts) == 2 {
						result[key] = parts[1]  // Full key-value mode
					}
				}
			}
		}
	default:
		// File namespace
		if strings.HasPrefix(namespace, ".") || strings.Contains(namespace, "/") {
			k.parseEnvFile(namespace, pattern, result, keysOnly)
		}
	}
	
	return result
}

// parseEnvFile reads and parses an env file, extracting matching keys/values
func (k *kevOps) parseEnvFile(path, pattern string, result map[string]string, keysOnly bool) {
	if !File.Exists(path) {
		return
	}
	
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	
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
					result[key] = ""  // Keys only mode
				} else if len(parts) == 2 {
					value := strings.TrimSpace(parts[1])
					value = strings.Trim(value, `"'`)
					result[key] = value  // Full key-value mode
				}
			}
		}
	}
}

// getAllFromNamespace gets all key-value pairs from a namespace matching pattern
func (k *kevOps) getAllFromNamespace(namespace, pattern string) map[string]string {
	return k.getNamespaceData(namespace, pattern, false)
}

// keysFromNamespace gets all keys from a namespace matching pattern
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
//   KEV.All()         // {"memory": {...}} - just memory
//   KEV.All("API_*")  // {"memory": {...}, "os": {...}, ".env": {...}} - filtered
//   KEV.All("os:*")   // {"os": {...}} - just os namespace
//   KEV.All("*:*")    // {"memory": {...}, "os": {...}, ".env": {...}} - everything
//
// Examples:
//   // Debug what's loaded
//   fmt.Println(KEV.All())
//
//   // Compare across namespaces
//   all := KEV.All("*:*")
//   if all["os"]["DEBUG"] != all[".env"]["DEBUG"] {
//       fmt.Println("Environment mismatch!")
//   }
func (k *kevOps) All(patterns ...string) map[string]map[string]string {
	result := make(map[string]map[string]string)
	
	if len(patterns) == 0 {
		// Default - return memory only
		k.mu.RLock()
		if len(k.memory) > 0 {
			memCopy := make(map[string]string)
			for key, val := range k.memory {
				memCopy[key] = val
			}
			result["memory"] = memCopy
		}
		k.mu.RUnlock()
		return result
	}
	
	// Check if any pattern has namespace
	hasNamespace := false
	for _, pattern := range patterns {
		if strings.Contains(pattern, ":") {
			hasNamespace = true
			break
		}
	}
	
	if !hasNamespace {
		// No namespaces - get from memory and all sources
		k.mu.RLock()
		memMatches := make(map[string]string)
		for key, val := range k.memory {
			for _, pattern := range patterns {
				if matchPattern(key, pattern) {
					memMatches[key] = val
					break
				}
			}
		}
		if len(memMatches) > 0 {
			result["memory"] = memMatches
		}
		sources := k.sources
		k.mu.RUnlock()
		
		// Check all sources
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
	
	// Special case for *:* - get all namespaces
	if len(patterns) == 1 && patterns[0] == "*:*" {
		// Add memory
		k.mu.RLock()
		if len(k.memory) > 0 {
			memCopy := make(map[string]string)
			for key, val := range k.memory {
				memCopy[key] = val
			}
			result["memory"] = memCopy
		}
		k.mu.RUnlock()
		
		// Add all sources
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
		// Process specific patterns
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
//   KEV.Clear()           // Clear all memory
//   KEV.Clear("API_*")    // Clear API_* from memory only
func (k *kevOps) Clear(patterns ...string) {
	// Only allow memory clearing for safety
	for _, pattern := range patterns {
		if strings.Contains(pattern, ":") {
			panic(Error("Clear() with namespace is dangerous! Use ClearUnsafe() if you really need this."))
		}
	}
	
	k.mu.Lock()
	defer k.mu.Unlock()
	
	if len(patterns) == 0 {
		// Clear all memory
		k.memory = make(map[string]string)
		return
	}
	
	// Clear patterns from memory only
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
//   KEV.ClearUnsafe("os:TEMP_*")     // Remove TEMP_* from OS
//   KEV.ClearUnsafe("os:DEBUG")      // Remove specific OS var
func (k *kevOps) ClearUnsafe(patterns ...string) {
	for _, pattern := range patterns {
		namespace, keyPattern := parseKey(pattern)
		
		if namespace == "" {
			// No namespace - just use regular Clear
			k.Clear(pattern)
			continue
		}
		
		switch namespace {
		case "os":
			if keyPattern == "*" {
				panic(Error("ClearUnsafe(\"os:*\") would destroy system! This is never allowed."))
			}
			keys := k.keysFromNamespace("os", keyPattern)
			for _, key := range keys {
				os.Unsetenv(key)
			}
		default:
			// File namespace - would need to rewrite file
			panic(Error("Clearing from files not yet implemented"))
		}
	}
}

// Unset removes specific keys
func (k *kevOps) Unset(keys ...string) {
	for _, key := range keys {
		namespace, realKey := parseKey(key)
		
		if namespace != "" {
			// Namespaced unset
			switch namespace {
			case "os":
				os.Unsetenv(realKey)
			default:
				// File - would need to rewrite file
				panic(Error("Unsetting from files not yet implemented"))
			}
		} else {
			// Unnamespaced - remove from memory
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
//   port := KEV.Int("PORT", 8080)
//   workers := KEV.Int("os:WORKERS", 4)
func (k *kevOps) Int(key string, defaultValue int) int {
	val := k.Get(key)
	if val == "" {
		return defaultValue
	}
	
	i, err := strconv.Atoi(val)
	if err != nil {
		panic(Error("invalid int value for", key, ":", val))
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
		panic(Error("invalid bool value for", key, ":", val))
	}
}

// Duration returns environment variable as time.Duration with default
func (k *kevOps) Duration(key string, defaultValue time.Duration) time.Duration {
	val := k.Get(key)
	if val == "" {
		return defaultValue
	}
	
	d, err := time.ParseDuration(val)
	if err != nil {
		panic(Error("invalid duration value for", key, ":", val))
	}
	return d
}

// Export saves all memory variables to a file
func (k *kevOps) Export(path string) {
	k.mu.RLock()
	var lines []string
	for key, val := range k.memory {
		// Escape values with spaces or special chars
		if strings.ContainsAny(val, " \t\n\"'") {
			val = fmt.Sprintf("%q", val)
		}
		lines = append(lines, fmt.Sprintf("%s=%s", key, val))
	}
	k.mu.RUnlock()
	
	content := strings.Join(lines, "\n")
	File.Write(path, []byte(content))
}

// Dump prints all environment variables (masks sensitive keys)
func (k *kevOps) Dump() {
	all := k.All()
	
	// Always a nested map now
	for namespace, vars := range all {
		for key, val := range vars {
			// Mask sensitive values
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
//   // Remove OS for testing
//   KEV.Source.Remove("os")
//   KEV.Get("PATH")  // Now only checks memory → .env
//
//   // Add more sources
//   KEV.Source.Add(".env.local", ".env.production")
//   KEV.Get("API_KEY")  // Checks: memory → os → .env → .env.local → .env.production
//
//   // Replace entirely
//   KEV.Source.Set(".env.test")
//   KEV.Get("API_KEY")  // Only checks memory → .env.test
type sourceOps struct {
	kev *kevOps
}

// Set replaces all sources
func (s sourceOps) Set(sources ...string) {
	s.kev.mu.Lock()
	defer s.kev.mu.Unlock()
	s.kev.sources = sources
}

// Add adds sources to the search list
func (s sourceOps) Add(sources ...string) {
	s.kev.mu.Lock()
	defer s.kev.mu.Unlock()
	s.kev.sources = append(s.kev.sources, sources...)
}

// Remove removes specific sources
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

// List returns current sources
func (s sourceOps) List() []string {
	s.kev.mu.RLock()
	defer s.kev.mu.RUnlock()
	
	result := make([]string, len(s.kev.sources))
	copy(result, s.kev.sources)
	return result
}

// Clear removes all sources
func (s sourceOps) Clear() {
	s.kev.mu.Lock()
	defer s.kev.mu.Unlock()
	s.kev.sources = []string{}
}

// getFromFile reads a specific key from a file
func (k *kevOps) getFromFile(path string, key string) string {
	if !File.Exists(path) {
		return ""
	}
	
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			fileKey := strings.TrimSpace(parts[0])
			if fileKey == key {
				value := strings.TrimSpace(parts[1])
				// Remove quotes if present
				value = strings.Trim(value, `"'`)
				return value
			}
		}
	}
	return ""
}

// setToFile writes or updates a key in a file
func (k *kevOps) setToFile(path string, key string, value string) {
	// Read existing content
	var lines []string
	found := false
	
	if File.Exists(path) {
		content := File.Read(path)
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			
			// Skip empty and comments
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				lines = append(lines, line)
				continue
			}
			
			// Check if this is the key we're updating
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) >= 1 {
				fileKey := strings.TrimSpace(parts[0])
				if fileKey == key {
					// Update existing key
					if strings.ContainsAny(value, " \t\n\"'") {
						value = fmt.Sprintf("%q", value)
					}
					lines = append(lines, fmt.Sprintf("%s=%s", key, value))
					found = true
				} else {
					lines = append(lines, line)
				}
			} else {
				lines = append(lines, line)
			}
		}
	}
	
	// If key wasn't found, append it
	if !found {
		if strings.ContainsAny(value, " \t\n\"'") {
			value = fmt.Sprintf("%q", value)
		}
		lines = append(lines, fmt.Sprintf("%s=%s", key, value))
	}
	
	// Write back
	content := strings.Join(lines, "\n")
	File.Write(path, []byte(content))
}

// matchPattern matches a key against a pattern (supports * wildcard)
func matchPattern(key, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(key, prefix)
	}
	if strings.HasPrefix(pattern, "*") {
		suffix := pattern[1:]
		return strings.HasSuffix(key, suffix)
	}
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		if len(parts) == 2 {
			return strings.HasPrefix(key, parts[0]) && strings.HasSuffix(key, parts[1])
		}
	}
	return key == pattern
}