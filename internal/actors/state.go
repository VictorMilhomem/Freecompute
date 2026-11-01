package actors

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type PersistedTaskState struct {
	JobID      string `json:"job_id"`
	Nonce      []byte `json:"nonce"`
	ResultHash []byte `json:"result_hash"`
	OutputDir  string `json:"output_dir"`
	ExitCode   int32  `json:"exit_code"`
}

// Persisting nonce and result_hash with fsync+rename before posting ResultCommit
// guarantees a restarted worker can still reveal and avoid being penalized.
func SaveTaskState(stateDir string, st PersistedTaskState) error {
	if stateDir == "" || st.JobID == "" {
		return nil
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(stateDir, st.JobID+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	target := filepath.Join(stateDir, st.JobID+".json")
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func LoadTaskState(stateDir, jobID string) (*PersistedTaskState, error) {
	if stateDir == "" || jobID == "" {
		return nil, nil
	}
	target := filepath.Join(stateDir, jobID+".json")
	data, err := os.ReadFile(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var st PersistedTaskState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func LoadAllTaskStates(stateDir string) (map[string]PersistedTaskState, error) {
	out := make(map[string]PersistedTaskState)
	if stateDir == "" {
		return out, nil
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(stateDir, e.Name()))
		if err != nil {
			continue
		}
		var st PersistedTaskState
		if err := json.Unmarshal(data, &st); err == nil && st.JobID != "" {
			out[st.JobID] = st
		}
	}
	return out, nil
}

func RemoveTaskState(stateDir, jobID string) error {
	if stateDir == "" || jobID == "" {
		return nil
	}
	target := filepath.Join(stateDir, jobID+".json")
	err := os.Remove(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
