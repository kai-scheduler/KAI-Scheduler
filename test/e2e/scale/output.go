// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"encoding/json"
	"os"

	. "github.com/onsi/ginkgo/v2"
)

const (
	outputFileName = "kwok_scale_test.json"
)

type TestResult struct {
	TestName string      `json:"test_name"`
	Status   string      `json:"status"`
	Details  interface{} `json:"details"`
}

type SuiteResult struct {
	Status string       `json:"status"`
	Tests  []TestResult `json:"tests"`
}

func writeTestResults(testName string, success bool, data interface{}) error {
	testResult := TestResult{
		TestName: testName,
		Status:   "failure",
		Details:  data,
	}

	suiteResult := "failure"
	if success {
		testResult.Status = "success"
		suiteResult = "success"
	}

	// write to file
	file, err := os.OpenFile(outputFileName, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		GinkgoLogr.Error(err, "Failed to open file")
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		GinkgoLogr.Error(err, "Failed to stat file")
		return err
	}

	currentDataBytes := make([]byte, info.Size())

	_, err = file.Read(currentDataBytes)
	if err != nil {
		GinkgoLogr.Error(err, "Failed to read file")
		return err
	}

	var previousData SuiteResult
	err = json.Unmarshal(currentDataBytes, &previousData)
	if err != nil {
		GinkgoLogr.Info("Failed to unmarshal file", "error", err)
		previousData = SuiteResult{
			Status: suiteResult,
			Tests:  []TestResult{},
		}
	}

	previousData.Status = suiteResult
	if testName != "" {
		previousData.Tests = append(previousData.Tests, testResult)
	}
	bytes, err := json.Marshal(previousData)
	if err != nil {
		return err
	}

	_, err = file.Seek(0, 0)
	if err != nil {
		GinkgoLogr.Error(err, "Failed to seek file")
		return err
	}
	_, err = file.Write(bytes)
	if err != nil {
		GinkgoLogr.Error(err, "Failed to write file")
	}
	if err := file.Truncate(int64(len(bytes))); err != nil {
		GinkgoLogr.Error(err, "Failed to truncate file")
		return err
	}
	return err
}
