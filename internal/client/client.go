// Package client es un cliente HTTP minimalista para la API de AuraNode.
// Añade el bearer token, normaliza errores de la API y decodifica JSON.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string // p.ej. https://api.auranode.app
	token   string
	http    *http.Client
}

// New crea un cliente. baseURL puede o no incluir el sufijo /api/v1.
func New(baseURL, token string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/api/v1")
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError representa un error devuelto por la API ({error, message}).
type APIError struct {
	Status  int
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// Do ejecuta una petición a /api/v1<path>; si body != nil se envía como JSON.
// out (puntero) recibe la respuesta decodificada si no es nil.
func (c *Client) Do(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}

	url := c.baseURL + "/api/v1" + path
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("no se pudo contactar el backend: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		apiErr := &APIError{Status: resp.StatusCode}
		_ = json.Unmarshal(data, apiErr) // best-effort
		return apiErr
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("respuesta inesperada del backend: %w", err)
		}
	}
	return nil
}

func (c *Client) Get(path string, out any) error  { return c.Do(http.MethodGet, path, nil, out) }
func (c *Client) Post(path string, body, out any) error {
	return c.Do(http.MethodPost, path, body, out)
}
func (c *Client) Delete(path string, out any) error {
	return c.Do(http.MethodDelete, path, nil, out)
}
