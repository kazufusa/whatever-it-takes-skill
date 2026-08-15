package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// GateConfig は setup が書き、run/verify が読む非秘匿の設定。
// 秘密鍵はここには入れない (環境変数 GATE_PRIVATE_KEY_SEED でのみ渡す)。
type GateConfig struct {
	Mode                string `json:"mode"` // "mechanical" または "claude"
	ProjectDir          string `json:"project_dir"`
	CheckCmd            string `json:"check_cmd,omitempty"`
	PromptFile          string `json:"prompt_file,omitempty"`
	QuietSeconds        int    `json:"quiet_seconds"`
	PollInterval        int    `json:"poll_interval"`
	MaxInterval         int    `json:"max_interval"`
	CheckTimeoutSeconds int    `json:"check_timeout_seconds"`
	Retention           int    `json:"retention"`
	MaxBudgetUSD        string `json:"max_budget_usd,omitempty"`
	JudgeModel          string `json:"judge_model,omitempty"`
	JudgeTools          string `json:"judge_tools,omitempty"`
}

func configPath(gateDir string) string {
	return filepath.Join(gateDir, "gate.conf.json")
}

func loadConfig(gateDir string) (*GateConfig, error) {
	b, err := os.ReadFile(configPath(gateDir))
	if err != nil {
		return nil, err
	}
	var c GateConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveConfig(gateDir string, c *GateConfig) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(gateDir), b, 0o644)
}
