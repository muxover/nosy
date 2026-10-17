package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// settings is the config file. A zero value means the setting is not set.
type settings struct {
	Chrome   string `json:"chrome,omitempty"`
	Device   string `json:"device,omitempty"`
	Duration int    `json:"duration,omitempty"` // seconds
	GPC      *bool  `json:"gpc,omitempty"`
}

type setting struct {
	name string
	help string
	def  string
	// set validates value and stores it
	set func(s *settings, value string) error
	get func(s settings) string
}

var allSettings = []setting{
	{
		name: "chrome", def: "", help: "path of Chrome or Chromium; empty searches the usual places",
		get: func(s settings) string { return s.Chrome },
		set: func(s *settings, v string) error { s.Chrome = v; return nil },
	},
	{
		name: "device", def: "desktop", help: "desktop or mobile",
		get: func(s settings) string { return s.Device },
		set: func(s *settings, v string) error {
			if _, err := parseDevice(v); err != nil {
				return err
			}
			s.Device = v
			return nil
		},
	},
	{
		name: "duration", def: "20", help: "longest a scan may last, in seconds, 10 to 120",
		get: func(s settings) string {
			if s.Duration == 0 {
				return ""
			}
			return strconv.Itoa(s.Duration)
		},
		set: func(s *settings, v string) error {
			d, err := parseDuration(v)
			if err != nil {
				return err
			}
			s.Duration = int(d / time.Second)
			return nil
		},
	},
	{
		name: "gpc", def: "false", help: "send the Global Privacy Control signal: true or false",
		get: func(s settings) string {
			if s.GPC == nil {
				return ""
			}
			return strconv.FormatBool(*s.GPC)
		},
		set: func(s *settings, v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return errors.New(`the value must be "true" or "false"`)
			}
			s.GPC = &b
			return nil
		},
	},
}

func findSetting(name string) (setting, bool) {
	for _, s := range allSettings {
		if s.name == name {
			return s, true
		}
	}
	return setting{}, false
}

func settingNames() string {
	var names []string
	for _, s := range allSettings {
		names = append(names, s.name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// configFile finds the config file; a test build can point it elsewhere.
var configFile = defaultConfigPath

func defaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("no config directory on this system: %w", err)
	}
	return filepath.Join(dir, "nosy", "config.json"), nil
}

// loadSettings reads the config file. A file that does not exist is an empty config.
func (a *app) loadSettings() (settings, error) {
	var s settings
	path, err := a.configPath()
	if err != nil {
		return s, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s is not valid: %w", path, err)
	}
	return s, nil
}

func (a *app) saveSettings(s settings) error {
	path, err := a.configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

const configUsage = `Usage: nosy config <command>

Commands:
  list              show every setting and its value
  get <key>         show one setting
  set <key> <value> change a setting
  path              show where the config file is

Settings:
`

func (a *app) config(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "-help" {
		out := a.out
		if len(args) == 0 {
			out = a.err
		}
		fmt.Fprint(out, configUsage)
		for _, s := range allSettings {
			fmt.Fprintf(out, "  %-9s %s (default %s)\n", s.name, s.help, defaultText(s))
		}
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	switch args[0] {
	case "path":
		if len(args) != 1 {
			return a.usageError("config", errors.New("path takes no arguments"))
		}
		p, err := a.configPath()
		if err != nil {
			return a.fail(err)
		}
		fmt.Fprintln(a.out, p)
		return exitOK
	case "list":
		if len(args) != 1 {
			return a.usageError("config", errors.New("list takes no arguments"))
		}
		cur, err := a.loadSettings()
		if err != nil {
			return a.fail(err)
		}
		for _, s := range allSettings {
			fmt.Fprintf(a.out, "%-9s %s\n", s.name, valueText(s, cur))
		}
		return exitOK
	case "get":
		if len(args) != 2 {
			return a.usageError("config", errors.New("get takes one key"))
		}
		s, ok := findSetting(args[1])
		if !ok {
			return a.usageError("config", fmt.Errorf("no setting %q; the settings are %s", args[1], settingNames()))
		}
		cur, err := a.loadSettings()
		if err != nil {
			return a.fail(err)
		}
		fmt.Fprintln(a.out, effective(s, cur))
		return exitOK
	case "set":
		if len(args) != 3 {
			return a.usageError("config", errors.New("set takes a key and a value"))
		}
		s, ok := findSetting(args[1])
		if !ok {
			return a.usageError("config", fmt.Errorf("no setting %q; the settings are %s", args[1], settingNames()))
		}
		cur, err := a.loadSettings()
		if err != nil {
			return a.fail(err)
		}
		if err := s.set(&cur, args[2]); err != nil {
			return a.usageError("config", fmt.Errorf("%s: %w", s.name, err))
		}
		if err := a.saveSettings(cur); err != nil {
			return a.fail(err)
		}
		fmt.Fprintf(a.out, "%s = %s\n", s.name, effective(s, cur))
		return exitOK
	}
	return a.usageError("config", fmt.Errorf("unknown command %q", args[0]))
}

// effective is the value in force: the one set, or the default.
func effective(s setting, cur settings) string {
	if v := s.get(cur); v != "" {
		return v
	}
	return s.def
}

func defaultText(s setting) string {
	if s.def == "" {
		return "empty"
	}
	return s.def
}

func valueText(s setting, cur settings) string {
	if v := s.get(cur); v != "" {
		return v
	}
	return defaultText(s) + " (default)"
}

// fail reports an error that is not the user's usage and returns the failure code.
func (a *app) fail(err error) int {
	fmt.Fprintf(a.err, "nosy: %v\n", err)
	return exitFailed
}
