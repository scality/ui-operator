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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type MockServerClient struct {
	namespace   string
	serviceName string
	servicePort int
}

func NewMockServerClient(namespace, serviceName string) *MockServerClient {
	return &MockServerClient{
		namespace:   namespace,
		serviceName: serviceName,
		servicePort: MockServerServicePort,
	}
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

	args := []string{
		"run", "-i", "--rm", "--restart=Never",
		"--namespace", c.namespace,
		"curl-client", "--image=curlimages/curl:latest",
		"--", "curl", "-s", "-X", method,
	}

	if body != "" {
		args = append(args, "-H", "Content-Type: application/json", "-d", body)
	}
	args = append(args, url)

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("curl failed: %w, stderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
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
