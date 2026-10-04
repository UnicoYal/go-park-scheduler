package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Message struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      Chat   `json:"chat"`
	From      *User  `json:"from"`
}

type Chat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

type User struct {
	ID int64 `json:"id"`
}

type Update struct {
	UpdateID     int64              `json:"update_id"`
	Message      *Message           `json:"message"`
	MyChatMember *ChatMemberUpdated `json:"my_chat_member"`
}

type ChatMemberUpdated struct {
	Chat          Chat       `json:"chat"`
	From          User       `json:"from"`
	OldChatMember ChatMember `json:"old_chat_member"`
	NewChatMember ChatMember `json:"new_chat_member"`
}

type ChatMember struct {
	Status string `json:"status"`
	User   User   `json:"user"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(token string, httpClient *http.Client) *Client {
	return &Client{baseURL: "https://api.telegram.org/bot" + token, http: httpClient}
}

func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	var updates []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         50,
		"allowed_updates": []string{"message", "my_chat_member"},
	}, &updates)
	return updates, err
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	_, err := c.SendMessageTracked(ctx, chatID, text)
	return err
}

func (c *Client) SendMessageTracked(ctx context.Context, chatID int64, text string) (int64, error) {
	var message Message
	err := c.call(ctx, "sendMessage", map[string]any{
		"chat_id": chatID,
		"text":    text,
	}, &message)
	if err != nil {
		return 0, err
	}
	return message.MessageID, nil
}

func (c *Client) DeleteMessage(ctx context.Context, chatID, messageID int64) error {
	return c.call(ctx, "deleteMessage", map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	}, nil)
}

func (c *Client) LeaveChat(ctx context.Context, chatID int64) error {
	return c.call(ctx, "leaveChat", map[string]any{"chat_id": chatID}, nil)
}

func (c *Client) call(ctx context.Context, method string, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		safeError := strings.ReplaceAll(err.Error(), c.baseURL, "https://api.telegram.org/bot<redacted>")
		return fmt.Errorf("telegram %s request failed: %s", method, safeError)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return fmt.Errorf("telegram returned HTTP %d: %w", resp.StatusCode, err)
	}
	if !envelope.OK {
		return fmt.Errorf("telegram %s: %s", method, envelope.Description)
	}
	if result != nil {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("decode telegram %s result: %w", method, err)
		}
	}
	return nil
}
