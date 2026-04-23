package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// configFileName is the on-disk config file name.
const configFileName = "config.yaml"

// appDirName is the subdirectory under the user's config root.
const appDirName = "dbagent"

// DefaultPath returns the resolved config path per XDG, without
// checking if the file exists.
//
// On Unix-like systems, this is $XDG_CONFIG_HOME/dbagent/config.yaml
// if XDG_CONFIG_HOME is set, otherwise $HOME/.config/dbagent/config.yaml.
// On Windows, os.UserConfigDir is used to pick the OS-native location.
func DefaultPath() (string, error) {
	if runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config dir: %w", err)
		}
		return filepath.Join(dir, appDirName, configFileName), nil
	}

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appDirName, configFileName), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", appDirName, configFileName), nil
}
