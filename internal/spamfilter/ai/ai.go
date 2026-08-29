// Package ai implements the spamfilter.Classifier via any OpenAI-compatible
// chat-completions endpoint, using stdlib net/http only (no provider SDK).
package ai

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/bossm8/formelay/internal/render"
	"github.com/bossm8/formelay/internal/spamfilter"
	"github.com/bossm8/formelay/internal/yamlutil"
)

const Type = "ai"

//go:embed defaults/system.tmpl
var defaultSystemTemplate string

//go:embed defaults/user.tmpl
var defaultUserTemplate string

var verdictRE = regexp.MustCompile(`^VERDICT:\s*(SPAM|NOT_SPAM)\s*$`)

// Config is decoded from a form's `spam_filter.provider` block. Prompt
// template selection is handled separately by TemplateSource, since it comes
// from the form-level spam_filter block, not this provider block.
type Config struct {
	APIBase   string            `yaml:"api_base"`
	APIKeyEnv string            `yaml:"api_key_env"`
	Model     string            `yaml:"model"`
	Timeout   yamlutil.Duration `yaml:"timeout"`
}

func (c *Config) Validate() error {
	if c.APIBase == "" {
		return errors.New("spamfilter/ai: 'api_base' is required")
	}
	if c.APIKeyEnv == "" {
		return errors.New("spamfilter/ai: 'api_key_env' is required")
	}
	if os.Getenv(c.APIKeyEnv) == "" {
		return fmt.Errorf("spamfilter/ai: environment variable %q referenced by 'api_key_env' is not set", c.APIKeyEnv)
	}
	if c.Model == "" {
		return errors.New("spamfilter/ai: 'model' is required")
	}
	return nil
}

type classifier struct {
	cfg        Config
	client     *http.Client
	systemTmpl *render.Template
	userTmpl   *render.Template
}

// New is a spamfilter.NewClassifierFunc. prompts carries the form's
// already-resolved system/user prompt source text (file-read or inline,
// resolved by the orchestration layer); an empty field falls back to the
// embedded, injection-hardened default below.
func New(raw map[string]any, prompts spamfilter.PromptSource) (spamfilter.Classifier, error) {
	cfg := Config{Timeout: yamlutil.Duration(10 * time.Second)}
	if err := yamlutil.Decode(raw, &cfg); err != nil {
		return nil, fmt.Errorf("spamfilter/ai: decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	systemSource := defaultSystemTemplate
	if prompts.SystemSource != "" {
		systemSource = prompts.SystemSource
	}
	userSource := defaultUserTemplate
	if prompts.UserSource != "" {
		userSource = prompts.UserSource
	}

	systemTmpl, err := render.Parse(render.KindText, "spam-filter-system", systemSource)
	if err != nil {
		return nil, err
	}
	userTmpl, err := render.Parse(render.KindText, "spam-filter-user", userSource)
	if err != nil {
		return nil, err
	}

	return &classifier{
		cfg:        cfg,
		client:     &http.Client{Timeout: cfg.Timeout.Std()},
		systemTmpl: systemTmpl,
		userTmpl:   userTmpl,
	}, nil
}

func (c *classifier) Type() string { return Type }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func (c *classifier) Classify(ctx context.Context, data render.SubmissionData) (spamfilter.Verdict, error) {
	systemMsg, err := c.systemTmpl.Execute(data)
	if err != nil {
		return spamfilter.Verdict{}, fmt.Errorf("spamfilter/ai: render system prompt: %w", err)
	}
	userMsg, err := c.userTmpl.Execute(data)
	if err != nil {
		return spamfilter.Verdict{}, fmt.Errorf("spamfilter/ai: render user prompt: %w", err)
	}

	reqBody, err := json.Marshal(chatRequest{
		Model: c.cfg.Model,
		Messages: []chatMessage{
			{Role: "system", Content: string(systemMsg)},
			{Role: "user", Content: string(userMsg)},
		},
	})
	if err != nil {
		return spamfilter.Verdict{}, fmt.Errorf("spamfilter/ai: encode request: %w", err)
	}

	url := strings.TrimRight(c.cfg.APIBase, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return spamfilter.Verdict{}, fmt.Errorf("spamfilter/ai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv(c.cfg.APIKeyEnv))

	resp, err := c.client.Do(req)
	if err != nil {
		return spamfilter.Verdict{}, fmt.Errorf("spamfilter/ai: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return spamfilter.Verdict{}, fmt.Errorf("spamfilter/ai: read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return spamfilter.Verdict{}, fmt.Errorf("spamfilter/ai: unexpected status %d", resp.StatusCode)
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return spamfilter.Verdict{}, fmt.Errorf("spamfilter/ai: parse response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return spamfilter.Verdict{}, errors.New("spamfilter/ai: no choices in response")
	}

	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	firstLine := strings.SplitN(content, "\n", 2)[0]
	m := verdictRE.FindStringSubmatch(strings.TrimSpace(firstLine))
	if m == nil {
		return spamfilter.Verdict{}, fmt.Errorf("spamfilter/ai: model response did not match the required VERDICT contract")
	}
	return spamfilter.Verdict{IsSpam: m[1] == "SPAM", Reason: content}, nil
}
