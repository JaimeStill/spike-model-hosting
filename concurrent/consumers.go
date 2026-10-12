package concurrent

import (
	"context"
	"errors"
	"fmt"

	"github.com/JaimeStill/spike-harness-driver/harness"
	"github.com/JaimeStill/spike-harness-driver/model"
)

// ModelChat is the model client's chat stream: one OpenAI chat completion per prompt.
type ModelChat struct {
	Client *model.Client
	Model  string
	// MaxTokens caps the completion, reasoning included; zero leaves it to the server.
	MaxTokens int
}

// Chat sends prompt as one user message and returns the reply's text.
func (m ModelChat) Chat(ctx context.Context, prompt string) (string, error) {
	resp, err := m.Client.Chat(ctx, model.ChatRequest{
		Model:               m.Model,
		Messages:            []model.Message{{Role: model.RoleUser, Parts: []model.Part{model.Text{Text: prompt}}}},
		MaxCompletionTokens: m.MaxTokens,
	})
	if err != nil {
		return "", err
	}
	if resp.Text == "" {
		return "", fmt.Errorf("the reply's text is empty (finish_reason %q)", resp.FinishReason)
	}
	return resp.Text, nil
}

// ModelEmbed is the model client's embedding stream.
type ModelEmbed struct {
	Client *model.Client
	Model  string
}

// Embed embeds inputs in one request.
func (m ModelEmbed) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	resp, err := m.Client.Embed(ctx, model.EmbedRequest{Model: m.Model, Input: inputs})
	if err != nil {
		return nil, err
	}
	return resp.Vectors, nil
}

// HarnessChat is a coding agent's chat stream, driven through its harness.Driver: Pi's, or
// Claude Code's. Each prompt opens a session of its own, so no answer sees another request's
// nonce in its history, and a stream's workers never share a session, which carries one
// exchange at a time.
type HarnessChat struct {
	Driver harness.Driver
	// Options open each session: the provider and model, the working directory, and the
	// harness tools, which a run sets empty so the agent only answers.
	Options harness.Options
}

// Chat opens a session, sends prompt, and returns the exchange's text.
func (h HarnessChat) Chat(ctx context.Context, prompt string) (text string, err error) {
	s, err := h.Driver.Open(ctx, h.Options)
	if err != nil {
		return "", fmt.Errorf("opening the session: %w", err)
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	x, err := s.Send(ctx, harness.Request{Text: prompt})
	if err != nil {
		return "", err
	}
	res, err := x.Wait()
	if err != nil {
		return "", err
	}
	return res.Text, nil
}
