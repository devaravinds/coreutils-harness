package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultOpenAIBaseURL = "https://api.openai.com/v1"

// Client sends requests to the OpenAI API. It is safe for concurrent use.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a client.
// The default request timeout is 60 seconds.
func NewClient() *Client {
	var apiKey = os.Getenv("OPENAI_API_KEY")

	baseURL := os.Getenv("OPENAI_BASE_URL")
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultOpenAIBaseURL
	}

	client := &Client{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
	return client
}

// APIError describes a non-success response returned by the API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("OpenAI API returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("OpenAI API returned HTTP %d: %s", e.StatusCode, e.Body)
}

// CreateResponse calls POST /responses.
func (client *Client) CreateResponse(ctx context.Context, input ResponsesRequest) (*ResponsesResponse, error) {
	var output ResponsesResponse
	if err := client.doJSON(ctx, http.MethodPost, "/responses", input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

func (client *Client) doJSON(ctx context.Context, method, endpoint string, input, output any) error {
	if client == nil {
		return errors.New("OpenAI client is nil")
	}
	if client.apiKey == "" {
		return errors.New("OpenAI API key is missing; pass it to NewClient or set OPENAI_API_KEY")
	}
	if client.httpClient == nil {
		return errors.New("OpenAI HTTP client is nil")
	}

	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode OpenAI request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	url := strings.TrimRight(client.baseURL, "/") + "/" + strings.TrimLeft(endpoint, "/")
	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("create OpenAI request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.apiKey)
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send OpenAI request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		if readErr != nil {
			return fmt.Errorf("read OpenAI error response: %w", readErr)
		}
		return &APIError{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}

	if output == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return fmt.Errorf("decode OpenAI response: %w", err)
	}
	return nil
}

// ResponsesRequest contains the commonly used fields for POST /responses.
// Input accepts a string or structured input supported by the API.
type ResponsesRequest struct {
	Model              string `json:"model"`
	Input              any    `json:"input"`
	Instructions       string `json:"instructions,omitempty"`
	MaxOutputTokens    int    `json:"max_output_tokens,omitempty"`
	PreviousResponseID string `json:"previous_response_id,omitempty"`
	Tools              []Tool `json:"tools,omitempty"`
}

// Tool describes a function tool made available to the model.
type Tool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict,omitempty"`
}

// ResponsesResponse contains the response fields commonly needed by clients.
type ResponsesResponse struct {
	ID     string               `json:"id"`
	Model  string               `json:"model"`
	Status string               `json:"status"`
	Output []ResponseOutputItem `json:"output"`
}

// ResponseOutputItem is an item in a Responses API output array.
type ResponseOutputItem struct {
	Type      string                `json:"type"`
	Role      string                `json:"role,omitempty"`
	Content   []ResponseContentPart `json:"content,omitempty"`
	CallID    string                `json:"call_id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
}

// FunctionCallOutput is the result of a function call returned to the model.
type FunctionCallOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

// ResponseContentPart is a content part within an output item.
type ResponseContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// OutputText concatenates the text output parts from a Responses API result.
func (response *ResponsesResponse) OutputText() string {
	if response == nil {
		return ""
	}
	var text strings.Builder
	for _, item := range response.Output {
		for _, part := range item.Content {
			if part.Type == "output_text" || part.Type == "text" {
				text.WriteString(part.Text)
			}
		}
	}
	return text.String()
}
