package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		message string
	}{
		{"invalid JSON", `{`, "decode configuration"},
		{"wrong header type", `{"Redirects":[{"Host":"example.com","To":"http://localhost","CopyHeaders":"Authorization"}]}`, "decode configuration"},
		{"no redirects", `{}`, "at least one redirect"},
		{"empty host", `{"Redirects":[{"To":"http://localhost"}]}`, "nonempty Host"},
		{"relative target", `{"Redirects":[{"Host":"example.com","To":"/relative"}]}`, "must use http or https"},
		{"invalid scheme", `{"Redirects":[{"Host":"example.com","To":"ftp://localhost"}]}`, "must use http or https"},
		{"invalid URL", `{"Redirects":[{"Host":"example.com","To":"http://["}]}`, "parse upstream URL"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(test.payload), 0600); err != nil {
				t.Fatal(err)
			}
			defer func() {
				message := fmt.Sprint(recover())
				if !strings.Contains(message, path) || !strings.Contains(message, test.message) {
					t.Errorf("received error %q, expected file %q and %q", message, path, test.message)
				}
			}()
			LoadConfig(path)
			t.Error("invalid configuration accepted")
		})
	}
}

func TestBundledConfigurationsLoad(t *testing.T) {
	for _, path := range []string{"config.json", "config_json.example"} {
		t.Run(path, func(t *testing.T) {
			configuration := LoadConfig(path)
			if len(configuration.Redirects) == 0 {
				t.Fatal("no redirects in configuration")
			}
		})
	}
}

func TestSaveConfigReportsWriteFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("config.json", 0700); err != nil {
		t.Fatal(err)
	}
	defer func() {
		message := fmt.Sprint(recover())
		if !strings.Contains(message, "write configuration config.json") {
			t.Errorf("received error %q, expected write failure with filename", message)
		}
	}()
	SaveConfig(&Config{})
	t.Error("configuration write failure ignored")
}
