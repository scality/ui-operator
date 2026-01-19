/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package framework

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const curlImage = "curlimages/curl:8.6.0"

func randomString(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(bytes)[:n]
}

type MockServerClient struct {
	namespace    string
	serviceName  string
	servicePort  int
	curlPodNames map[string]string
	mu           sync.Mutex
}

func NewMockServerClient(namespace, serviceName string) *MockServerClient {
	return &MockServerClient{
		namespace:    namespace,
		serviceName:  serviceName,
		servicePort:  MockServerServicePort,
		curlPodNames: map[string]string{},
	}
}

func (c *MockServerClient) curlPodName(namespace string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if name, ok := c.curlPodNames[namespace]; ok {
		return name
	}

	name := fmt.Sprintf("curl-%s", randomString(8))
	c.curlPodNames[namespace] = name
	return name
}

func (c *MockServerClient) ensureCurlPod(ctx context.Context, namespace string) (string, error) {
	podName := c.curlPodName(namespace)

	if err := c.waitForCurlPodReady(ctx, namespace, podName); err == nil {
		return podName, nil
	}

	args := []string{
		"run", podName,
		"--image=" + curlImage,
		"--namespace", namespace,
		"--restart=Never",
		"--image-pull-policy=IfNotPresent",
		"--command",
		"--",
		"sleep", "3600",
	}
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if !strings.Contains(stderr.String(), "AlreadyExists") {
			return "", fmt.Errorf("failed to create curl pod: %w, stderr: %s", err, stderr.String())
		}
	}

	if err := c.waitForCurlPodReady(ctx, namespace, podName); err != nil {
		return "", err
	}

	return podName, nil
}

func (c *MockServerClient) waitForCurlPodReady(ctx context.Context, namespace, podName string) error {
	args := []string{
		"wait",
		"--for=condition=Ready",
		"pod/" + podName,
		"--namespace", namespace,
		"--timeout=30s",
	}
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("curl pod not ready: %w, stderr: %s", err, stderr.String())
	}
	return nil
}

func (c *MockServerClient) execCurlCommand(ctx context.Context, fromNamespace string, curlArgs []string) (string, error) {
	podName, err := c.ensureCurlPod(ctx, fromNamespace)
	if err != nil {
		return "", err
	}

	args := []string{"exec", podName, "--namespace", fromNamespace, "--"}
	args = append(args, curlArgs...)

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("curl failed: %w, stderr: %s, stdout: %s", err, stderr.String(), stdout.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

func (c *MockServerClient) CleanupCurlPods(ctx context.Context) error {
	c.mu.Lock()
	podNames := make(map[string]string)
	for k, v := range c.curlPodNames {
		podNames[k] = v
	}
	c.mu.Unlock()

	var lastErr error
	for namespace, podName := range podNames {
		args := []string{"delete", "pod", podName, "--namespace", namespace, "--ignore-not-found"}
		cmd := exec.CommandContext(ctx, "kubectl", args...)
		if err := cmd.Run(); err != nil {
			lastErr = err
		}
	}

	c.mu.Lock()
	c.curlPodNames = map[string]string{}
	c.mu.Unlock()

	return lastErr
}

func (c *MockServerClient) GetCounter(ctx context.Context) (int64, error) {
	output, err := c.execCurl(ctx, "GET", "/_/counter", "")
	if err != nil {
		return 0, err
	}

	var result struct {
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return 0, fmt.Errorf("failed to parse counter response: %w", err)
	}
	return result.Count, nil
}

func (c *MockServerClient) Reset(ctx context.Context) error {
	_, err := c.execCurl(ctx, "POST", "/_/reset", "")
	return err
}

// TestFetch makes a test request to verify the mock server's current response
// Returns the HTTP status code and response body
func (c *MockServerClient) TestFetch(ctx context.Context) (int, string, error) {
	return c.TestFetchFromNamespace(ctx, c.namespace)
}

// TestFetchFromNamespace makes a test request from a specific namespace
// This helps diagnose cross-namespace connectivity issues
func (c *MockServerClient) TestFetchFromNamespace(ctx context.Context, fromNamespace string) (int, string, error) {
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/.well-known/micro-app-configuration",
		c.serviceName, c.namespace, c.servicePort)

	output, err := c.execCurlCommand(ctx, fromNamespace, []string{"curl", "-s", "-w", "\n%{http_code}", url})
	if err != nil {
		return 0, "", err
	}

	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return 0, output, fmt.Errorf("unexpected output format: %s", output)
	}

	// Last line is the status code
	statusCodeStr := strings.TrimSpace(lines[len(lines)-1])
	statusCode := 0
	if _, err := fmt.Sscanf(statusCodeStr, "%d", &statusCode); err != nil {
		return 0, output, fmt.Errorf("failed to parse status code %q: %w", statusCodeStr, err)
	}

	// Everything before the last line is the body
	body := strings.Join(lines[:len(lines)-1], "\n")

	return statusCode, body, nil
}

func (c *MockServerClient) SetConfig(ctx context.Context, delay int, statusCode int, response string) error {
	config := map[string]interface{}{}
	if delay > 0 {
		config["delay"] = delay
	}
	if statusCode > 0 {
		config["statusCode"] = statusCode
	}
	if response != "" {
		config["response"] = response
	}

	body, err := json.Marshal(config)
	if err != nil {
		return err
	}

	_, err = c.execCurl(ctx, "POST", "/_/config", string(body))
	return err
}

func (c *MockServerClient) SetDelay(ctx context.Context, delayMs int) error {
	return c.SetConfig(ctx, delayMs, 0, "")
}

func (c *MockServerClient) SetStatusCode(ctx context.Context, statusCode int) error {
	return c.SetConfig(ctx, 0, statusCode, "")
}

func (c *MockServerClient) SetResponse(ctx context.Context, response string) error {
	return c.SetConfig(ctx, 0, 0, response)
}

func (c *MockServerClient) execCurl(ctx context.Context, method, path, body string) (string, error) {
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s", c.serviceName, c.namespace, c.servicePort, path)

	curlArgs := []string{"curl", "-s", "-X", method}

	if body != "" {
		curlArgs = append(curlArgs, "-H", "Content-Type: application/json", "-d", body)
	}
	curlArgs = append(curlArgs, url)

	output, err := c.execCurlCommand(ctx, c.namespace, curlArgs)
	if err != nil {
		return "", err
	}
	output = extractJSON(output)

	return output, nil
}

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		return s
	}

	depth := 0
	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inString {
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

type MockServerDirectClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewMockServerDirectClient(baseURL string) *MockServerDirectClient {
	return &MockServerDirectClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *MockServerDirectClient) GetCounter(ctx context.Context) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/_/counter", nil)
	if err != nil {
		return 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Count int64 `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.Count, nil
}

func (c *MockServerDirectClient) Reset(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/_/reset", nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *MockServerDirectClient) SetConfig(ctx context.Context, delay int, statusCode int, response string) error {
	config := map[string]interface{}{}
	if delay > 0 {
		config["delay"] = delay
	}
	if statusCode > 0 {
		config["statusCode"] = statusCode
	}
	if response != "" {
		config["response"] = response
	}

	body, err := json.Marshal(config)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/_/config", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}
