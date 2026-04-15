package handler

import "net/http"

// CopilotModel describes an available model for the Copilot agent.
type CopilotModel struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	CostMultiplier string `json:"cost_multiplier"`
	IsDefault   bool    `json:"is_default,omitempty"`
	IsInternal  bool    `json:"is_internal,omitempty"`
}

// ListCopilotModels returns the known models available via the GitHub Copilot CLI.
func (h *Handler) ListCopilotModels(w http.ResponseWriter, r *http.Request) {
	models := []CopilotModel{
		{ID: "claude-opus-4.6-1m", Name: "Claude Opus 4.6 (1M context)(Internal only)", CostMultiplier: "6x", IsDefault: true, IsInternal: true},
		{ID: "claude-sonnet-4.6", Name: "Claude Sonnet 4.6", CostMultiplier: "1x"},
		{ID: "claude-sonnet-4.5", Name: "Claude Sonnet 4.5", CostMultiplier: "1x"},
		{ID: "claude-haiku-4.5", Name: "Claude Haiku 4.5", CostMultiplier: "0.33x"},
		{ID: "claude-opus-4.6", Name: "Claude Opus 4.6", CostMultiplier: "3x"},
		{ID: "claude-opus-4.5", Name: "Claude Opus 4.5", CostMultiplier: "3x"},
		{ID: "claude-sonnet-4", Name: "Claude Sonnet 4", CostMultiplier: "1x"},
		{ID: "goldeneye", Name: "Goldeneye (Internal Only)", CostMultiplier: "1x", IsInternal: true},
		{ID: "gpt-5.4", Name: "GPT-5.4", CostMultiplier: "1x"},
		{ID: "gpt-5.3-codex", Name: "GPT-5.3-Codex", CostMultiplier: "1x"},
		{ID: "gpt-5.2-codex", Name: "GPT-5.2-Codex", CostMultiplier: "1x"},
		{ID: "gpt-5.2", Name: "GPT-5.2", CostMultiplier: "1x"},
		{ID: "gpt-5.1", Name: "GPT-5.1", CostMultiplier: "1x"},
		{ID: "gpt-5.4-mini", Name: "GPT-5.4 mini", CostMultiplier: "0.33x"},
		{ID: "gpt-5-mini", Name: "GPT-5 mini", CostMultiplier: "0x"},
		{ID: "gpt-4.1", Name: "GPT-4.1", CostMultiplier: "0.33x"},
	}
	writeJSON(w, http.StatusOK, models)
}
