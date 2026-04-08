package main

import "strings"

func resolveOpenAIBaseURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/v1") {
		baseURL = strings.TrimSuffix(baseURL, "/v1")
	}
	return baseURL
}

func buildOpenAIURL(baseURL, path string) string {
	baseURL = resolveOpenAIBaseURL(baseURL)
	if path == "" {
		return baseURL
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseURL + path
}

func buildOpenAIResponsesURL(baseURL string) string {
	return buildOpenAIURL(baseURL, "/v1/responses")
}

func buildOpenAIModelsURL(baseURL string) string {
	return buildOpenAIURL(baseURL, "/v1/models")
}
