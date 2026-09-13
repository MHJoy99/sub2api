package apicompat

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

const shortJoyVoiceWAVBase64 = "UklGRjQAAABXQVZFZm10IBAAAAABAAEAgD4AAAB9AAACABAAZGF0YRAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestChatCompletionsToResponsesPreservesInputAudio(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "joyvoice-fast-audio",
		Messages: []ChatMessage{{
			Role:    "user",
			Content: json.RawMessage(`[{"type":"text","text":"Transcribe this"},{"type":"input_audio","input_audio":{"data":"` + shortJoyVoiceWAVBase64 + `","format":"wav"}}]`),
		}},
	}

	responses, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(responses.Input, &items))
	require.Len(t, items, 1)

	var parts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[0].Content, &parts))
	require.Len(t, parts, 2)
	require.Equal(t, "input_audio", parts[1].Type)
	require.NotNil(t, parts[1].InputAudio)
	require.Equal(t, shortJoyVoiceWAVBase64, parts[1].InputAudio.Data)
	require.Equal(t, "wav", parts[1].InputAudio.Format)
}

func TestInputAudioSurvivesResponsesAndAnthropicBridges(t *testing.T) {
	data := shortJoyVoiceWAVBase64
	req := &ChatCompletionsRequest{
		Model: "joyvoice-fast-audio",
		Messages: []ChatMessage{{
			Role:    "user",
			Content: json.RawMessage(`[{"type":"text","text":"Transcribe this"},{"type":"input_audio","input_audio":{"data":"` + data + `","format":"wav"}}]`),
		}},
	}

	responses, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	anthropic, err := ResponsesToAnthropicRequest(responses)
	require.NoError(t, err)
	var anthropicBlocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(anthropic.Messages[0].Content, &anthropicBlocks))
	require.Len(t, anthropicBlocks, 2)
	require.Equal(t, "audio", anthropicBlocks[1].Type)
	require.NotNil(t, anthropicBlocks[1].Source)
	require.Equal(t, "audio/wav", anthropicBlocks[1].Source.MediaType)
	require.Equal(t, data, anthropicBlocks[1].Source.Data)

	fromAnthropic, err := AnthropicToResponses(anthropic)
	require.NoError(t, err)
	var roundTripItems []ResponsesInputItem
	require.NoError(t, json.Unmarshal(fromAnthropic.Input, &roundTripItems))
	var roundTripParts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(roundTripItems[0].Content, &roundTripParts))
	require.Len(t, roundTripParts, 2)
	require.Equal(t, "input_audio", roundTripParts[1].Type)
	require.Equal(t, data, roundTripParts[1].InputAudio.Data)
	require.Equal(t, "wav", roundTripParts[1].InputAudio.Format)

	fromResponsesToChat, err := ResponsesToChatCompletionsRequest(responses)
	require.NoError(t, err)
	var chatParts []ChatContentPart
	require.NoError(t, json.Unmarshal(fromResponsesToChat.Messages[0].Content, &chatParts))
	require.Len(t, chatParts, 2)
	require.Equal(t, "input_audio", chatParts[1].Type)
	require.Equal(t, data, chatParts[1].InputAudio.Data)

	fromAnthropicToChat, err := AnthropicToChatCompletionsRequest(anthropic)
	require.NoError(t, err)
	require.Len(t, fromAnthropicToChat.Messages, 1)
	var directChatParts []ChatContentPart
	require.NoError(t, json.Unmarshal(fromAnthropicToChat.Messages[0].Content, &directChatParts))
	require.Len(t, directChatParts, 2)
	require.Equal(t, "input_audio", directChatParts[1].Type)
	require.Equal(t, data, directChatParts[1].InputAudio.Data)
}

func TestInputAudioValidationRejectsMissingOrInvalidData(t *testing.T) {
	for _, data := range []string{"", "not-base64!!!"} {
		req := &ChatCompletionsRequest{
			Model: "joyvoice-fast-audio",
			Messages: []ChatMessage{{
				Role:    "user",
				Content: json.RawMessage(`[{"type":"input_audio","input_audio":{"data":"` + data + `","format":"wav"}}]`),
			}},
		}
		_, err := ChatCompletionsToResponses(req)
		require.Error(t, err)
	}
}

func TestShortJoyVoiceWAVFixtureIsValidBase64(t *testing.T) {
	wav, err := base64.StdEncoding.DecodeString(shortJoyVoiceWAVBase64)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(wav), 44)
	require.Equal(t, []byte("RIFF"), wav[:4])
	require.Equal(t, []byte("WAVE"), wav[8:12])
	require.Equal(t, []byte("fmt "), wav[12:16])
	require.Equal(t, []byte("data"), wav[36:40])
}
