package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"yipyip/internal/config"
)

// Question types of the System One (jev) endpoint.
const (
	JevChoice = "choice"
	JevScore  = "score"
	JevNoul   = "noul"
)

// ErrJev is wrapped by every error returned by JevClient.Ask.
var ErrJev = errors.New("jev request failed")

// JevQuestion is one question sent to jev.
type JevQuestion struct {
	Name         string            // key in "questions"
	Type         string            // JevChoice, JevScore or JevNoul
	Instructions string            // "instructions"
	Choices      map[string]string // choice: label -> description, 1..255 entries
	Levels       []string          // score: ordered criteria, 1..10 entries
}

// JevAnswer is the parsed answer to one JevQuestion.
type JevAnswer struct {
	Name, Type string
	Choice     string  // choice
	Score      float64 // score
	Confidence float64 // choice, score
	Noul       float64 // noul: probability 0..1
}

// JevUsage holds the token counts of one Ask.
type JevUsage struct{ Input, Output int }

// JevClient asks questions of the jev System One endpoint.
type JevClient struct {
	hc      *http.Client
	baseURL string
	apiKey  string
	model   string
	own     *config.Redactor // masks apiKey in every error text
}

// maxJevBody bounds the size of a jev response body.
const maxJevBody = 1 << 20

// NewJevClient returns a client for baseURL. hc must come from NewHTTPClient.
func NewJevClient(hc *http.Client, baseURL, apiKey, model string) *JevClient {
	return &JevClient{hc: hc, baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model, own: config.NewRedactor(apiKey)}
}

// Ask sends state and qs to jev and returns the answers in the order of qs.
// Every error wraps ErrJev and never contains the API key.
func (c *JevClient) Ask(ctx context.Context, state string, qs []JevQuestion) ([]JevAnswer, JevUsage, error) {
	ans, u, err := c.ask(ctx, state, qs)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil && !errors.Is(err, cerr) {
			err = fmt.Errorf("%w (%w)", cerr, err)
		}
		err = fmt.Errorf("%w: %w", ErrJev, err)
		return nil, JevUsage{}, &redactedError{msg: c.own.Redact(err.Error()), err: err}
	}
	return ans, u, nil
}

// validateJev checks qs before any request is made.
func validateJev(qs []JevQuestion) error {
	if len(qs) == 0 {
		return errors.New("no questions")
	}
	seen := make(map[string]bool, len(qs))
	for i, q := range qs {
		if q.Name == "" {
			return fmt.Errorf("question %d: empty name", i+1)
		}
		if seen[q.Name] {
			return fmt.Errorf("question %q: duplicate name", q.Name)
		}
		seen[q.Name] = true
		switch q.Type {
		case JevChoice:
			if n := len(q.Choices); n < 1 || n > 255 {
				return fmt.Errorf("question %q: %d choices, want 1..255", q.Name, n)
			}
		case JevScore:
			if n := len(q.Levels); n < 1 || n > 10 {
				return fmt.Errorf("question %q: %d levels, want 1..10", q.Name, n)
			}
		case JevNoul:
		default:
			return fmt.Errorf("question %q: unknown type %q", q.Name, q.Type)
		}
	}
	return nil
}

func (c *JevClient) body(state string, qs []JevQuestion) ([]byte, error) {
	questions := make(map[string]any, len(qs))
	for _, q := range qs {
		obj := map[string]any{"type": q.Type, "instructions": q.Instructions}
		switch q.Type {
		case JevChoice:
			obj["criteria"] = q.Choices
		case JevScore:
			obj["criteria"] = q.Levels
		}
		questions[q.Name] = obj
	}
	return json.Marshal(map[string]any{"model": c.model, "state": state, "questions": questions})
}

type jevReply struct {
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   *struct {
		Input  int `json:"input_tokens"`
		Output int `json:"output_tokens"`
	} `json:"usage"`
}

type jevWireAnswer struct {
	Type       string   `json:"type"`
	Choice     *string  `json:"choice"`
	Score      *float64 `json:"score"`
	Confidence *float64 `json:"confidence"`
	Noul       *float64 `json:"noul"`
}

func (c *JevClient) ask(ctx context.Context, state string, qs []JevQuestion) ([]JevAnswer, JevUsage, error) {
	if err := validateJev(qs); err != nil {
		return nil, JevUsage{}, err
	}
	payload, err := c.body(state, qs)
	if err != nil {
		return nil, JevUsage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/systemone", bytes.NewReader(payload))
	if err != nil {
		return nil, JevUsage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, JevUsage{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxJevBody+1))
	if err != nil {
		return nil, JevUsage{}, fmt.Errorf("reading response: %w", err)
	}
	if len(raw) > maxJevBody {
		return nil, JevUsage{}, fmt.Errorf("response larger than %d bytes", maxJevBody)
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnprocessableEntity:
		return nil, JevUsage{}, jevDetailError(raw)
	default:
		return nil, JevUsage{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var rep jevReply
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, JevUsage{}, fmt.Errorf("invalid response: %w", err)
	}
	out := make([]JevAnswer, 0, len(qs))
	for _, q := range qs {
		a, err := parseJevAnswer(q, rep.Answers[q.Name])
		if err != nil {
			return nil, JevUsage{}, err
		}
		out = append(out, a)
	}
	var u JevUsage
	if rep.Usage != nil {
		u = JevUsage{Input: rep.Usage.Input, Output: rep.Usage.Output}
	} else {
		u = JevUsage{Input: max(1, len(payload)/4), Output: max(1, len(raw)/4)}
	}
	return out, u, nil
}

// jevDetailError builds the error of a 422 reply from its detail[].msg texts.
func jevDetailError(raw []byte) error {
	var d struct {
		Detail []struct {
			Msg string `json:"msg"`
		} `json:"detail"`
	}
	var msgs []string
	if json.Unmarshal(raw, &d) == nil {
		for _, e := range d.Detail {
			if e.Msg != "" {
				msgs = append(msgs, e.Msg)
			}
		}
	}
	if len(msgs) == 0 {
		return errors.New("HTTP 422")
	}
	return fmt.Errorf("HTTP 422: %s", strings.Join(msgs, "; "))
}

// parseJevAnswer decodes the answer to q; a missing answer, a different type
// or a missing value is an error.
func parseJevAnswer(q JevQuestion, raw json.RawMessage) (JevAnswer, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return JevAnswer{}, fmt.Errorf("answer %q missing", q.Name)
	}
	var w jevWireAnswer
	if err := json.Unmarshal(raw, &w); err != nil {
		return JevAnswer{}, fmt.Errorf("answer %q: %w", q.Name, err)
	}
	if w.Type != q.Type {
		return JevAnswer{}, fmt.Errorf("answer %q: type %q, want %q", q.Name, w.Type, q.Type)
	}
	a := JevAnswer{Name: q.Name, Type: q.Type}
	switch q.Type {
	case JevChoice:
		if w.Choice == nil {
			return JevAnswer{}, fmt.Errorf("answer %q: no choice", q.Name)
		}
		a.Choice = *w.Choice
	case JevScore:
		if w.Score == nil {
			return JevAnswer{}, fmt.Errorf("answer %q: no score", q.Name)
		}
		a.Score = *w.Score
	case JevNoul:
		if w.Noul == nil {
			return JevAnswer{}, fmt.Errorf("answer %q: no noul", q.Name)
		}
		a.Noul = *w.Noul
	}
	if w.Confidence != nil {
		a.Confidence = *w.Confidence
	}
	return a, nil
}
