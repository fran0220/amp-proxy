package main

import "github.com/tidwall/gjson"

// ParseClaudeUsage extracts token usage from a Claude API response or SSE data event.
// Claude format: {"usage":{"input_tokens":N,"output_tokens":N,"cache_read_input_tokens":N,"cache_creation_input_tokens":N}}
func ParseClaudeUsage(data []byte) TokenUsage {
	usage := gjson.GetBytes(data, "usage")
	if !usage.Exists() {
		return TokenUsage{}
	}
	return TokenUsage{
		InputTokens:       usage.Get("input_tokens").Int(),
		OutputTokens:      usage.Get("output_tokens").Int(),
		CacheReadTokens:   usage.Get("cache_read_input_tokens").Int(),
		CacheCreateTokens: usage.Get("cache_creation_input_tokens").Int(),
	}
}

// ParseOpenAIUsage extracts token usage from an OpenAI API response or SSE data event.
// OpenAI format: {"usage":{"prompt_tokens":N,"completion_tokens":N,"total_tokens":N}}
// Responses API: {"usage":{"input_tokens":N,"output_tokens":N}}
func ParseOpenAIUsage(data []byte) TokenUsage {
	usage := gjson.GetBytes(data, "usage")
	if !usage.Exists() {
		return TokenUsage{}
	}

	input := usage.Get("input_tokens").Int()
	if input == 0 {
		input = usage.Get("prompt_tokens").Int()
	}
	output := usage.Get("output_tokens").Int()
	if output == 0 {
		output = usage.Get("completion_tokens").Int()
	}
	cacheRead := usage.Get("prompt_tokens_details.cached_tokens").Int()
	if cacheRead == 0 {
		cacheRead = usage.Get("input_tokens_details.cached_tokens").Int()
	}

	return TokenUsage{
		InputTokens:     input,
		OutputTokens:    output,
		CacheReadTokens: cacheRead,
	}
}

// ParseGeminiUsage extracts token usage from a Gemini API response.
// Gemini format: {"usageMetadata":{"promptTokenCount":N,"candidatesTokenCount":N,"cachedContentTokenCount":N}}
func ParseGeminiUsage(data []byte) TokenUsage {
	usage := gjson.GetBytes(data, "usageMetadata")
	if !usage.Exists() {
		return TokenUsage{}
	}
	return TokenUsage{
		InputTokens:     usage.Get("promptTokenCount").Int(),
		OutputTokens:    usage.Get("candidatesTokenCount").Int(),
		CacheReadTokens: usage.Get("cachedContentTokenCount").Int(),
	}
}
