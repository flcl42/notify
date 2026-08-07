package adb

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

func FindAdb(explicitPath string) string {
	candidates := []string{explicitPath}
	if env := os.Getenv("ADB"); env != "" {
		candidates = append(candidates, env)
	}

	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		userProfile := os.Getenv("USERPROFILE")
		if localAppData != "" {
			candidates = append(candidates, filepath.Join(localAppData, "Android", "Sdk", "platform-tools", "adb.exe"))
		}
		if userProfile != "" {
			candidates = append(candidates, filepath.Join(userProfile, "AppData", "Local", "Android", "Sdk", "platform-tools", "adb.exe"))
		}
		candidates = append(candidates, "adb.exe")
	} else {
		candidates = append(candidates, "adb")
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if strings.HasSuffix(candidate, ".exe") || runtime.GOOS == "windows" {
			if _, err := os.Stat(candidate); err != nil {
				continue
			}
		}
		cmd := exec.Command(candidate, "version")
		if err := cmd.Run(); err == nil {
			return candidate
		}
	}

	return ""
}

func HasConnectedDevice(adbPath string) bool {
	out, err := exec.Command(adbPath, "devices").Output()
	if err != nil {
		return false
	}
	re := regexp.MustCompile(`\tdevice\b`)
	for _, line := range strings.Split(string(out), "\n") {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func SetupReverse(adbPath string, port int) error {
	cmd := exec.Command(adbPath, "reverse", fmt.Sprintf("tcp:%d", port), fmt.Sprintf("tcp:%d", port))
	return cmd.Run()
}
