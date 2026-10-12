package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/vlm"
)

const (
	maxChatGPTVisionImages    = 20
	maxChatGPTVisionImageSize = 20 << 20
	maxChatGPTVisionTotalSize = 50 << 20
)

// personalChatGPTVLM adapts the credential-refreshing personal chat client to
// the VLM interface. The inner client is intentionally resolved by
// personalChatGPTChat on every call, so disconnect and token rotation take
// effect immediately.
type personalChatGPTVLM struct {
	chat chat.Chat
}

func (v *personalChatGPTVLM) GetModelName() string { return v.chat.GetModelName() }
func (v *personalChatGPTVLM) GetModelID() string   { return v.chat.GetModelID() }

func (v *personalChatGPTVLM) Predict(ctx context.Context, images [][]byte, prompt string) (string, error) {
	if len(images) == 0 {
		return "", errors.New("ChatGPT vision requires at least one image")
	}
	if len(images) > maxChatGPTVisionImages {
		return "", fmt.Errorf("ChatGPT vision accepts at most %d images per request", maxChatGPTVisionImages)
	}

	parts := make([]chat.MessageContentPart, 0, len(images)+1)
	parts = append(parts, chat.MessageContentPart{Type: "text", Text: prompt})
	total := 0
	for _, image := range images {
		if len(image) == 0 {
			return "", errors.New("ChatGPT vision image is empty")
		}
		if len(image) > maxChatGPTVisionImageSize {
			return "", fmt.Errorf("ChatGPT vision image exceeds %d MiB", maxChatGPTVisionImageSize>>20)
		}
		total += len(image)
		if total > maxChatGPTVisionTotalSize {
			return "", fmt.Errorf("ChatGPT vision images exceed %d MiB in total", maxChatGPTVisionTotalSize>>20)
		}
		mime, err := chatGPTVisionMIME(image)
		if err != nil {
			return "", err
		}
		parts = append(parts, chat.MessageContentPart{
			Type: "image_url",
			ImageURL: &chat.ImageURL{
				URL:    "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image),
				Detail: "auto",
			},
		})
	}

	response, err := v.chat.Chat(ctx, []chat.Message{{Role: "user", MultiContent: parts}}, nil)
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("ChatGPT vision returned an empty response")
	}
	if response.FinishReason == "length" {
		return "", fmt.Errorf("%w (finish_reason=length)", vlm.ErrTruncatedCompletion)
	}
	return response.Content, nil
}

func chatGPTVisionMIME(data []byte) (string, error) {
	switch mime := http.DetectContentType(data); mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return mime, nil
	default:
		return "", errors.New("ChatGPT vision supports PNG, JPEG, GIF, or WebP images")
	}
}
