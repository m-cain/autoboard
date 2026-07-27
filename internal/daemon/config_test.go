package daemon

import (
	"path/filepath"
	"testing"
)

func TestLoadConfigUsesPrivateLocalDefaults(t *testing.T) {
	config, err := loadConfig(
		filepath.Join("/Users/test", "Library", "Application Support"),
		func(string) string { return "" },
	)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	wantData := filepath.Join(
		"/Users/test",
		"Library",
		"Application Support",
		"Autoboard",
	)
	if config.Address != "127.0.0.1:4040" ||
		config.DataDir != wantData ||
		config.DatabasePath != filepath.Join(wantData, "autoboard.db") ||
		config.Development {
		t.Errorf("config = %#v", config)
	}
}

func TestLoadConfigSupportsScopedOverrides(t *testing.T) {
	environment := map[string]string{
		"AUTOBOARD_HTTP_PORT":            "4242",
		"AUTOBOARD_DATA_DIR":             "/tmp/autoboard-test",
		"AUTOBOARD_DATABASE_PATH":        "/tmp/autoboard-test/custom.db",
		"AUTOBOARD_MAX_ATTACHMENT_BYTES": "4096",
		"AUTOBOARD_DEVELOPMENT":          "1",
	}
	config, err := loadConfig("/ignored", func(key string) string {
		return environment[key]
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.Address != "127.0.0.1:4242" ||
		config.DataDir != environment["AUTOBOARD_DATA_DIR"] ||
		config.DatabasePath != environment["AUTOBOARD_DATABASE_PATH"] ||
		config.MaxAttachmentBytes != 4096 ||
		!config.Development {
		t.Errorf("config = %#v", config)
	}
}

func TestLoadConfigRejectsInvalidNumericOverrides(t *testing.T) {
	for key, value := range map[string]string{
		"AUTOBOARD_HTTP_PORT":            "0",
		"AUTOBOARD_MAX_ATTACHMENT_BYTES": "-1",
	} {
		t.Run(key, func(t *testing.T) {
			_, err := loadConfig("/tmp", func(candidate string) string {
				if candidate == key {
					return value
				}
				return ""
			})
			if err == nil {
				t.Fatal("load config succeeded, want error")
			}
		})
	}
}

func TestLoadConfigRejectsDatabaseOutsidePrivateDataDirectory(t *testing.T) {
	_, err := loadConfig("/tmp", func(key string) string {
		switch key {
		case "AUTOBOARD_DATA_DIR":
			return "/tmp/autoboard-private"
		case "AUTOBOARD_DATABASE_PATH":
			return "/tmp/shared/autoboard.db"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("load config succeeded, want database containment error")
	}
}
