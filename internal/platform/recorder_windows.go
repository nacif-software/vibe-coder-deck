//go:build windows

package platform

import "errors"

type RecordedEvent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	At    int64  `json:"at"`
}

func StartRecording(mouse bool) error {
	return errors.New("recording is not available on Windows yet; use the Manual tab")
}

func StopRecording() {}

func Recording(since int) (bool, []RecordedEvent) { return false, nil }
