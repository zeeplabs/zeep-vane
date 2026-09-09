// Package openai implements vane's llm.Provider for OpenAI, generating SLO
// analysis text and validating credentials via OpenAI's API.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/llm"
)

const (
	defaultBaseURL      = "https://api.openai.com"
	chatCompletionsPath = "/v1/chat/completions"
	modelsPath          = "/v1/models"
	maxCompletionTokens = 200
	// defaultTimeout is resend.defaultTimeout (10s) doubled: an LLM chat
	// completion call is slower than a transactional email send (it
	// involves actual model inference, not just an API acknowledging a
	// queued send), so the same bound would risk classifying a merely-slow
	// (but otherwise healthy) completion as a timeout.
	defaultTimeout = 20 * time.Second
)

// Client generates completions through OpenAI's API using an API key,
// satisfying llm.Provider.
type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewClient builds a Client authenticated with apiKey, configured to use
// model, talking to the real OpenAI API.
func NewClient(apiKey, model string) *Client {
	return &Client{
		apiKey:     apiKey,
		model:      model,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// chatMessage is one message in a chat completion request/response.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatCompletionRequest mirrors OpenAI's documented POST
// /v1/chat/completions request shape (platform.openai.com/docs/api-reference/chat).
type chatCompletionRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
}

// chatCompletionResponse mirrors OpenAI's documented response shape - only
// the fields this client reads.
type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Complete sends systemPrompt and userPrompt to OpenAI's chat completions
// API and returns the first choice's message content.
func (c *Client) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	body := chatCompletionRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens: maxCompletionTokens,
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("openai: failed to encode chat completion request: %w", err)
	}

	resp, err := c.post(ctx, c.baseURL+chatCompletionsPath, encoded)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var parsed chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("openai: failed to decode chat completion response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openai: chat completion response had no choices")
	}

	return parsed.Choices[0].Message.Content, nil
}

// ValidateCredentials checks that the client's API key is valid, without
// generating any completion. It performs a minimal list-models lookup and
// only inspects the outcome; the response body's content is irrelevant
// here.
func (c *Client) ValidateCredentials(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+modelsPath, nil)
	if err != nil {
		return fmt.Errorf("openai: failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// post issues an authenticated POST with a JSON body against endpoint and
// classifies the outcome into vane's shared llm typed errors. On success it
// returns the response with its body still open for the caller.
func (c *Client) post(ctx context.Context, endpoint string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return c.do(req)
}

// do executes req and classifies the outcome into vane's shared llm typed
// errors.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if isTimeout(err) {
			return nil, llm.ErrTimeout
		}
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		resp.Body.Close()
		return nil, llm.ErrUnauthorized
	case resp.StatusCode >= http.StatusInternalServerError:
		resp.Body.Close()
		return nil, llm.ErrServer
	case resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
		return resp, nil
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("openai: unexpected status %d", resp.StatusCode)
	}
}

// isTimeout reports whether err represents a client-side timeout (either
// the context deadline or the http.Client's own Timeout firing).
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
