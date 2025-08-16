package kev_test

import (
	"testing"
	"strings"
	utils "github.com/adriangalilea/go-utils"
)

func TestSourceTracking(t *testing.T) {
	// Setup
	utils.KEV.Clear()
	utils.KEV.Source.Clear()
	utils.KEV.Source.Set("os", ".env", "../.env")
	
	// Test Set tracking
	utils.KEV.Set("MANUAL_KEY", "manual_value")
	source := utils.KEV.SourceOf("MANUAL_KEY")
	if source != "set" {
		t.Errorf("Expected source 'set', got '%s'", source)
	}
	
	// Test GetWithSource
	value, src := utils.KEV.GetWithSource("MISSING_KEY", "default_value")
	if value != "default_value" {
		t.Errorf("Expected value 'default_value', got '%s'", value)
	}
	if src != "default" {
		t.Errorf("Expected source 'default', got '%s'", src)
	}
	
	// Test Export with source comments
	utils.KEV.Export("/tmp/test_export.env")
	content := string(utils.File.Read("/tmp/test_export.env"))
	if !strings.Contains(content, "# from:") {
		t.Error("Export should include source comments")
	}
	
	// Test All with source info
	all := utils.KEV.All()
	if memory, exists := all["memory"]; exists {
		for _, val := range memory {
			if !strings.Contains(val, "[from:") {
				t.Error("All() should include source info in values")
			}
		}
	}
}

func TestDebugMode(t *testing.T) {
	// Clear and enable debug
	utils.KEV.Clear()
	utils.KEV.Debug = true
	defer func() { utils.KEV.Debug = false }()
	
	// This should not cause infinite recursion
	_ = utils.KEV.Get("TEST_KEY", "default")
	
	// Verify LOG_LEVEL doesn't cause issues
	_ = utils.KEV.Get("LOG_LEVEL", "info")
}

func TestMemorySourcePersistence(t *testing.T) {
	utils.KEV.Clear()
	utils.KEV.Source.Clear()
	utils.KEV.Source.Set(".env")
	
	// First get should cache with source
	val := utils.KEV.Get("TEST_KEY")
	if val != "from_test_env" {
		t.Errorf("Expected 'from_test_env', got '%s'", val)
	}
	
	// Source should be .env
	source := utils.KEV.SourceOf("TEST_KEY")
	if source != ".env" {
		t.Errorf("Expected source '.env', got '%s'", source)
	}
	
	// Remove .env from sources
	utils.KEV.Source.Clear()
	
	// Should still get value from memory with correct source
	val2 := utils.KEV.Get("TEST_KEY")
	if val2 != "from_test_env" {
		t.Errorf("Expected cached 'from_test_env', got '%s'", val2)
	}
	
	source2 := utils.KEV.SourceOf("TEST_KEY")
	if source2 != ".env" {
		t.Errorf("Cached source should still be '.env', got '%s'", source2)
	}
}

func TestParentDirectoryEnv(t *testing.T) {
	utils.KEV.Clear()
	utils.KEV.Source.Clear()
	utils.KEV.Source.Set(".env", "../.env")
	
	// Should find in current dir first
	val := utils.KEV.Get("TEST_KEY")
	if val != "from_test_env" {
		t.Errorf("Expected 'from_test_env', got '%s'", val)
	}
	source := utils.KEV.SourceOf("TEST_KEY")
	if source != ".env" {
		t.Errorf("Expected source '.env', got '%s'", source)
	}
	
	// Should find parent-only key in parent
	parentVal := utils.KEV.Get("PARENT_ONLY")
	if parentVal != "parent_value" {
		t.Errorf("Expected 'parent_value', got '%s'", parentVal)
	}
	parentSource := utils.KEV.SourceOf("PARENT_ONLY")
	if parentSource != "../.env" {
		t.Errorf("Expected source '../.env', got '%s'", parentSource)
	}
}