package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const maxWebhookRetries = 2

type webhookClient struct {
	url    string
	client *http.Client
}

func newWebhookClient(url string) *webhookClient {
	return &webhookClient{
		url: strings.TrimSpace(url),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// doWithRetry executes the request and retries once on HTTP 429 rate-limit responses.
func (w *webhookClient) doWithRetry(ctx context.Context, buildReq func() (*http.Request, error)) error {
	for attempt := 0; attempt <= maxWebhookRetries; attempt++ {
		req, err := buildReq()
		if err != nil {
			return fmt.Errorf("failed to create webhook request: %w", err)
		}
		req.Header.Set("User-Agent", "Koolo Discord Webhook")

		resp, err := w.client.Do(req)
		if err != nil {
			return fmt.Errorf("webhook request failed: %w", err)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxWebhookRetries {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			select {
			case <-time.After(retryAfter):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if resp.StatusCode >= http.StatusBadRequest {
			return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		return nil
	}
	return fmt.Errorf("webhook rate-limited after %d retries", maxWebhookRetries+1)
}

// parseRetryAfter parses the Discord Retry-After header (seconds as float).
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 2 * time.Second
	}
	secs, err := strconv.ParseFloat(header, 64)
	if err != nil || secs <= 0 {
		return 2 * time.Second
	}
	// Cap the wait at 30 seconds to avoid indefinite blocking
	if secs > 30 {
		secs = 30
	}
	return time.Duration(secs*1000) * time.Millisecond
}

func (w *webhookClient) Send(ctx context.Context, content, fileName string, fileData []byte) error {
	return w.doWithRetry(ctx, func() (*http.Request, error) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)

		if err := writer.WriteField("content", content); err != nil {
			writer.Close()
			return nil, fmt.Errorf("failed to prepare webhook payload: %w", err)
		}

		if len(fileData) > 0 && fileName != "" {
			part, err := writer.CreateFormFile("file", fileName)
			if err != nil {
				writer.Close()
				return nil, fmt.Errorf("failed to add webhook file field: %w", err)
			}

			if _, err := part.Write(fileData); err != nil {
				writer.Close()
				return nil, fmt.Errorf("failed to write webhook file data: %w", err)
			}
		}

		contentType := writer.FormDataContentType()
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("failed to finalize webhook payload: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, &body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", contentType)
		return req, nil
	})
}

func (w *webhookClient) SendEmbed(ctx context.Context, embed *discordgo.MessageEmbed) error {
	return w.doWithRetry(ctx, func() (*http.Request, error) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)

		payload := struct {
			Embeds []*discordgo.MessageEmbed `json:"embeds"`
		}{
			Embeds: []*discordgo.MessageEmbed{embed},
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			writer.Close()
			return nil, fmt.Errorf("failed to serialize webhook embed: %w", err)
		}

		if err := writer.WriteField("payload_json", string(payloadJSON)); err != nil {
			writer.Close()
			return nil, fmt.Errorf("failed to prepare webhook embed payload: %w", err)
		}

		contentType := writer.FormDataContentType()
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("failed to finalize webhook embed payload: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, &body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", contentType)
		return req, nil
	})
}
