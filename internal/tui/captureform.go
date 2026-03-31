package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

type captureFormState struct {
	path string // file path (without .png extension)
}

func buildCaptureForm(state *captureFormState) *huh.Form {
	theme := huh.ThemeCharm()
	theme.Focused.Base = lipgloss.NewStyle().PaddingLeft(1)
	theme.Focused.Title = lipgloss.NewStyle().Foreground(colorPink).Bold(true)
	theme.Focused.Description = lipgloss.NewStyle().Foreground(colorMuted)
	theme.Focused.FocusedButton = lipgloss.NewStyle().Foreground(colorDarkBg).Background(colorPink).Bold(true).Padding(0, 1)
	theme.Focused.BlurredButton = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)
	theme.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(colorPink)
	theme.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(colorMagenta)
	theme.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(colorText)
	theme.Blurred = theme.Focused

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Save screenshot to").
				Value(&state.path).
				Validate(validateCapturePath),
		),
	).WithTheme(theme).WithWidth(60)
}

func validateCapturePath(s string) error {
	if s == "" {
		return fmt.Errorf("path required")
	}
	expanded := expandHome(s)
	dir := filepath.Dir(expanded)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("directory does not exist: %s", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}
	return nil
}

func defaultCapturePath(v view) string {
	name := strings.ToLower(viewName(v))
	ts := time.Now().Format("20060102-150405")
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, fmt.Sprintf("bewitch-%s-%s.png", name, ts))
}

// expandHome expands a leading ~ to the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}
