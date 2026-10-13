package services

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CookieRefresherService coordinates automated browser refreshes.
type CookieRefresherService struct {
	ProxyURL     string
	AsocksAPIKey string
	ProjectRoot  string
}

// NewCookieRefresherService creates a new CookieRefresherService.
func NewCookieRefresherService(proxyURL, asocksAPIKey string) *CookieRefresherService {
	// Resolve project root relative to the running binary (works correctly in Docker)
	execPath, err := os.Executable()
	projectRoot := "/app" // Safe default for Docker
	if err == nil {
		resolvedPath, err := filepath.EvalSymlinks(execPath)
		if err == nil {
			dir := filepath.Dir(resolvedPath)
			// If binary is at /app/goofish_backend, project root is /app
			if fi, err := os.Stat(filepath.Join(dir, "pw_refresher.py")); err == nil && !fi.IsDir() {
				projectRoot = dir
			}
		}
	}

	log.Printf("[CookieRefresher] Project root resolved to: %s", projectRoot)

	return &CookieRefresherService{
		ProxyURL:     strings.TrimSpace(proxyURL),
		AsocksAPIKey: strings.TrimSpace(asocksAPIKey),
		ProjectRoot:  projectRoot,
	}
}

// RefreshCookies invokes the Python Playwright micro-browser script to fetch new H5 MTOP cookies.
func (s *CookieRefresherService) RefreshCookies(currentCookies string) (string, error) {
	scriptPath := filepath.Join(s.ProjectRoot, "pw_refresher.py")

	// Verify script exists before attempting execution
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return "", fmt.Errorf("playwright refresher script not found at: %s", scriptPath)
	}

	log.Printf("[CookieRefresher] Invoking Playwright browser refresher script: %s", scriptPath)

	args := []string{
		scriptPath,
		"--cookies", currentCookies,
	}

	if s.ProxyURL != "" {
		args = append(args, "--proxy-url", s.ProxyURL)
	}

	if s.AsocksAPIKey != "" {
		args = append(args, "--asocks-key", s.AsocksAPIKey)
	}

	// Prepare exec Command
	cmd := exec.Command("python3", args...)
	cmd.Dir = s.ProjectRoot

	// Inherit ALL current environment variables so Playwright can find its browsers
	cmd.Env = os.Environ()

	// Only override PLAYWRIGHT_BROWSERS_PATH if the Docker path actually exists on disk.
	// On local Mac, leave it unset so Playwright uses its default ~/Library/Caches/ms-playwright.
	playwrightPath := os.Getenv("PLAYWRIGHT_BROWSERS_PATH")
	if playwrightPath == "" {
		// Try Docker default path
		dockerPath := "/app/pw-browsers"
		if _, err := os.Stat(dockerPath); err == nil {
			playwrightPath = dockerPath
		}
	}

	if playwrightPath != "" {
		// Verify path actually exists before injecting it
		if _, err := os.Stat(playwrightPath); err == nil {
			envFiltered := []string{}
			for _, e := range cmd.Env {
				if !strings.HasPrefix(e, "PLAYWRIGHT_BROWSERS_PATH=") {
					envFiltered = append(envFiltered, e)
				}
			}
			envFiltered = append(envFiltered, "PLAYWRIGHT_BROWSERS_PATH="+playwrightPath)
			cmd.Env = envFiltered
			log.Printf("[CookieRefresher] Using PLAYWRIGHT_BROWSERS_PATH: %s", playwrightPath)
		} else {
			// Path doesn't exist — strip the env var so Playwright uses its own default cache
			envFiltered := []string{}
			for _, e := range cmd.Env {
				if !strings.HasPrefix(e, "PLAYWRIGHT_BROWSERS_PATH=") {
					envFiltered = append(envFiltered, e)
				}
			}
			cmd.Env = envFiltered
			log.Printf("[CookieRefresher] PLAYWRIGHT_BROWSERS_PATH '%s' does not exist, using system default.", playwrightPath)
		}
	}

	// Capture stdout and stderr separately
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	stderrOutput := stderrBuf.String()
	stdoutOutput := stdoutBuf.String()

	// Print Python's stderr output to our Go console for full visibility
	if len(stderrOutput) > 0 {
		for _, line := range strings.Split(stderrOutput, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				log.Printf("[Playwright-Browser] %s", line)
			}
		}
	}

	if err != nil {
		return "", fmt.Errorf("playwright script execution failed: %w (stderr: %s)", err, stderrOutput)
	}

	refreshedCookies := strings.TrimSpace(stdoutOutput)
	if refreshedCookies == "" {
		return "", fmt.Errorf("playwright script returned empty cookies string")
	}

	log.Println("[CookieRefresher] Playwright browser successfully refreshed cookies!")
	return refreshedCookies, nil
}
