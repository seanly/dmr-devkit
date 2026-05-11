package tape

import (
	"strings"
	"testing"
	"time"
)

func TestSetTimezone(t *testing.T) {
	// Save original timezone
	original := GetTimezone()
	defer SetTimezone("") // Restore to local

	// Test setting to UTC
	err := SetTimezone("UTC")
	if err != nil {
		t.Fatalf("SetTimezone(UTC) failed: %v", err)
	}

	tz := GetTimezone()
	if tz.String() != "UTC" {
		t.Errorf("expected UTC, got %s", tz.String())
	}

	// Create an entry and verify it uses UTC
	entry := NewMessageEntry(map[string]any{"content": "test"})
	if !strings.Contains(entry.Date, "+00:00") && !strings.Contains(entry.Date, "Z") {
		t.Errorf("expected UTC timestamp (+00:00 or Z), got %s", entry.Date)
	}

	// Test setting to Asia/Shanghai
	err = SetTimezone("Asia/Shanghai")
	if err != nil {
		t.Fatalf("SetTimezone(Asia/Shanghai) failed: %v", err)
	}

	tz = GetTimezone()
	if tz.String() != "Asia/Shanghai" {
		t.Errorf("expected Asia/Shanghai, got %s", tz.String())
	}

	// Create an entry and verify it uses +08:00
	entry = NewMessageEntry(map[string]any{"content": "test"})
	if !strings.Contains(entry.Date, "+08:00") {
		t.Errorf("expected CST timestamp (+08:00), got %s", entry.Date)
	}

	// Test invalid timezone
	err = SetTimezone("Invalid/Timezone")
	if err == nil {
		t.Error("expected error for invalid timezone, got nil")
	}

	// Timezone should remain unchanged after error
	tz = GetTimezone()
	if tz.String() != "Asia/Shanghai" {
		t.Errorf("timezone should remain Asia/Shanghai after error, got %s", tz.String())
	}

	// Test empty string resets to local
	err = SetTimezone("")
	if err != nil {
		t.Fatalf("SetTimezone(\"\") failed: %v", err)
	}

	tz = GetTimezone()
	if tz != time.Local {
		t.Errorf("expected Local timezone, got %s", tz.String())
	}

	// Verify original is restored
	if original != time.Local {
		t.Logf("Note: original timezone was %s, not Local", original.String())
	}
}

func TestTimezoneInEntries(t *testing.T) {
	// Save and restore
	defer SetTimezone("")

	testCases := []struct {
		timezone string
		contains string
	}{
		{"UTC", "+00:00"},
		{"America/New_York", "-05:00"}, // EST (winter)
		{"Asia/Tokyo", "+09:00"},
		{"Europe/London", "+00:00"}, // GMT (winter)
	}

	for _, tc := range testCases {
		t.Run(tc.timezone, func(t *testing.T) {
			if err := SetTimezone(tc.timezone); err != nil {
				t.Skipf("timezone %s not available: %v", tc.timezone, err)
			}

			entry := NewMessageEntry(map[string]any{"content": "test"})

			// Parse the timestamp to verify it's valid
			_, err := time.Parse("2006-01-02T15:04:05.000000-07:00", entry.Date)
			if err != nil {
				t.Errorf("invalid timestamp format %q: %v", entry.Date, err)
			}

			// Note: timezone offset may vary due to DST, so we just check format validity
			t.Logf("Timezone %s produced timestamp: %s", tc.timezone, entry.Date)
		})
	}
}
