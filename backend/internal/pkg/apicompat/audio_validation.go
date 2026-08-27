package apicompat

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

var supportedChatInputAudioFormats = map[string]struct{}{
	"wav":       {},
	"mp3":       {},
	"ogg":       {},
	"flac":      {},
	"aac":       {},
	"webm":      {},
	"pcm16":     {},
	"g711_ulaw": {},
	"g711_alaw": {},
}

// ChatInputAudioMIMEType maps the OpenAI input_audio format to the MIME type
// expected by Gemini inlineData. Callers should validate the format first.
func ChatInputAudioMIMEType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "mp3":
		return "audio/mpeg"
	case "ogg":
		return "audio/ogg"
	case "flac":
		return "audio/flac"
	case "aac":
		return "audio/aac"
	case "webm":
		return "audio/webm"
	case "pcm16":
		return "audio/pcm"
	case "g711_ulaw", "g711_alaw":
		return "audio/basic"
	case "wav":
		fallthrough
	default:
		return "audio/wav"
	}
}

// ChatCompletionsRequestHasInputAudio reports whether a parsed Chat
// Completions request contains at least one input_audio content part.
func ChatCompletionsRequestHasInputAudio(req *ChatCompletionsRequest) bool {
	if req == nil {
		return false
	}
	for _, message := range req.Messages {
		var parts []ChatContentPart
		if err := unmarshalChatContentParts(message.Content, &parts); err != nil {
			continue
		}
		for _, part := range parts {
			if part.Type == "input_audio" {
				return true
			}
		}
	}
	return false
}

func unmarshalChatContentParts(raw []byte, parts *[]ChatContentPart) error {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return fmt.Errorf("content is empty")
	}
	return json.Unmarshal(raw, parts)
}

// validateChatContentParts rejects audio that would otherwise be silently
// dropped by a compatibility bridge. It also rejects unknown typed parts so a
// successful response can never claim to have processed content we discarded.
func validateChatContentParts(parts []ChatContentPart) error {
	for index, part := range parts {
		switch part.Type {
		case "text", "image_url", "file":
			continue
		case "input_audio":
			if err := validateChatInputAudio(part.InputAudio, index); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported content part type %q at index %d", part.Type, index)
		}
	}
	return nil
}

func validateChatInputAudio(audio *ChatInputAudio, index int) error {
	if audio == nil {
		return fmt.Errorf("input_audio content part %d is missing input_audio", index)
	}
	format := strings.ToLower(strings.TrimSpace(audio.Format))
	if format == "" {
		return fmt.Errorf("input_audio content part %d is missing format", index)
	}
	if _, ok := supportedChatInputAudioFormats[format]; !ok {
		return fmt.Errorf("input_audio content part %d has unsupported format", index)
	}
	data := strings.TrimSpace(audio.Data)
	if data == "" {
		return fmt.Errorf("input_audio content part %d is missing data", index)
	}
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		if _, rawErr := base64.RawStdEncoding.DecodeString(data); rawErr != nil {
			return fmt.Errorf("input_audio content part %d has invalid base64 data", index)
		}
	}
	return nil
}
