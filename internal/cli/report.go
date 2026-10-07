package cli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"hookcheck"
)

func writeReport(path string, report hookcheck.Report) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".hookcheck-report-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
