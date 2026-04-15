package handler

import "net/http"

// CopilotModel describes an available model for the Copilot agent.
type CopilotModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListCopilotModels returns the known models available via the GitHub Copilot CLI.
func (h *Handler) ListCopilotModels(w http.ResponseWriter, r *http.Request) {
	models := []CopilotModel{
		{ID: "gpt-5.4", Name: "GPT-5.4", Description: "Latest GPT model, best overall performance"},
		{ID: "gpt-5.4-mini", Name: "GPT-5.4 mini", Description: "Fast and cost-effective"},
		{ID: "gpt-5.2", Name: "GPT-5.2", Description: "Balanced performance and speed"},
		{ID: "claude-sonnet-4", Name: "Claude Sonnet 4", Description: "Anthropic's balanced model"},
		{ID: "claude-sonnet-4.5", Name: "Claude Sonnet 4.5", Description: "Enhanced Sonnet with extended thinking"},
		{ID: "o4-mini", Name: "o4-mini", Description: "OpenAI reasoning model, compact"},
		{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Description: "Google's advanced model"},
	}
	writeJSON(w, http.StatusOK, models)
}
