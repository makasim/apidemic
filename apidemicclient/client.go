package apidemicclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type API struct {
	Endpoint   string     `json:"endpoint"`
	HTTPMethod string     `json:"http_method"`
	Any        *Response  `json:"any,omitempty"`
	Exactly    []Response `json:"exactly,omitempty"`
}

type Response struct {
	Code    int         `json:"code"`
	Payload interface{} `json:"payload"`
}

type Client struct {
	HTTPHost string
	host     string
	port     int
	http     *http.Client
}

type HistoryEntry struct {
	Endpoint       string                 `json:"endpoint"`
	Body           string                 `json:"body"`
	Headers        map[string][]string    `json:"headers"`
	ResponseStatus int                    `json:"response_status"`
	ResponseBody   map[string]interface{} `json:"response_body"`
}

func New(host string, port int) *Client {
	return &Client{
		host:     host,
		port:     port,
		HTTPHost: fmt.Sprintf("http://%s:%d", host, port),
		http:     &http.Client{Timeout: time.Second * 2},
	}
}

func NewAndReset(host string, port int) *Client {
	c := &Client{
		host:     host,
		port:     port,
		HTTPHost: fmt.Sprintf("http://%s:%d", host, port),
		http:     &http.Client{Timeout: time.Second * 2},
	}

	c.MustReset()

	return c
}

func (c *Client) URL(endpoint string) string {
	return fmt.Sprintf("http://%s:%d%s", c.host, c.port, endpoint)
}

func (c *Client) RegisterAny(endpoint, httpMethod string, response interface{}, responseStatus int) error {
	return c.Register(API{
		Endpoint:   endpoint,
		HTTPMethod: httpMethod,
		Any: &Response{
			Code:    responseStatus,
			Payload: response,
		},
		Exactly: nil,
	})
}

func (c *Client) Register(api API) error {
	data, err := json.Marshal(api)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("http://%s:%d/_register", c.host, c.port),
		bytes.NewBuffer(data),
	)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("response status not OK, got %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) MustRegisterAny(endpoint, httpMethod string, response interface{}, responseStatus int) {
	if err := c.RegisterAny(endpoint, httpMethod, response, responseStatus); err != nil {
		panic(err)
	}
}

func (c *Client) MustRegister(api API) {
	if err := c.Register(api); err != nil {
		panic(err)
	}
}

func (c *Client) History() ([]HistoryEntry, error) {
	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("http://%s:%d/_history", c.host, c.port),
		http.NoBody,
	)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("response status not OK, got %d", resp.StatusCode)
	}

	history := make([]HistoryEntry, 0)
	err = json.NewDecoder(resp.Body).Decode(&history)
	if err != nil {
		return nil, err
	}

	return history, nil
}

func (c *Client) MustHistory() []HistoryEntry {
	history, err := c.History()
	if err != nil {
		panic(err)
	}

	return history
}

func (c *Client) HistoryFor(endpoint string) ([]HistoryEntry, error) {
	input, err := c.History()
	if err != nil {
		return nil, err
	}

	output := make([]HistoryEntry, 0)
	for _, he := range input {
		if he.Endpoint == endpoint {
			output = append(output, he)
		}
	}

	return output, nil
}

func (c *Client) MustHistoryFor(endpoint string) []HistoryEntry {
	output, err := c.HistoryFor(endpoint)
	if err != nil {
		panic(err)
	}

	return output
}

func (c *Client) Reset() error {
	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("http://%s:%d/_reset", c.host, c.port),
		http.NoBody,
	)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("close body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("response status not OK, got %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) MustReset() {
	err := c.Reset()
	if err != nil {
		panic(err)
	}
}
