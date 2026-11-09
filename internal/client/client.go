// Package client es el cliente HTTP compartido por la CLI y el MCP.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIError es un error HTTP con code estable.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (%s, HTTP %d)", e.Message, e.Code, e.Status)
	}
	return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Status)
}

// Client habla con la API de AgentBank Go.
type Client struct {
	BaseURL   string
	Token     string
	Project   string
	SessionID string
	HTTP      *http.Client
}

// New crea un cliente.
func New(baseURL, token, project string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		Project: project,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// RawResponse es una respuesta HTTP sin decodificar.
type RawResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

// DoRaw ejecuta un request y devuelve el cuerpo crudo.
func (c *Client) DoRaw(ctx context.Context, method, path string, query url.Values, headers map[string]string) (*RawResponse, error) {
	if c == nil || c.BaseURL == "" {
		return nil, fmt.Errorf("falta AGENTBANK_URL")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	target := c.BaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.SessionID != "" {
		request.Header.Set("X-Session-Id", c.SessionID)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		apiErr := &APIError{Status: response.StatusCode, Message: strings.TrimSpace(string(raw))}
		var decoded struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if json.Unmarshal(raw, &decoded) == nil && decoded.Error != "" {
			apiErr.Message = decoded.Error
			apiErr.Code = decoded.Code
		}
		return nil, apiErr
	}
	return &RawResponse{Status: response.StatusCode, Header: response.Header, Body: raw}, nil
}

// Do ejecuta un request y devuelve el JSON decodificado (o nil).
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any, headers map[string]string) (any, error) {
	if c == nil || c.BaseURL == "" {
		return nil, fmt.Errorf("falta AGENTBANK_URL")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	target := c.BaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("serializar body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.SessionID != "" {
		request.Header.Set("X-Session-Id", c.SessionID)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if session := response.Header.Get("X-Session-Id"); session != "" {
		c.SessionID = session
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		apiErr := &APIError{Status: response.StatusCode, Message: strings.TrimSpace(string(raw))}
		var decoded struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if json.Unmarshal(raw, &decoded) == nil && decoded.Error != "" {
			apiErr.Message = decoded.Error
			apiErr.Code = decoded.Code
		}
		return nil, apiErr
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("respuesta no JSON: %w", err)
	}
	return decoded, nil
}
