package main

import "testing"

func TestJobStoreClearPDFPassword(t *testing.T) {
	store := &JobStore{jobs: make(map[string]*Job)}
	store.jobs["job-1"] = &Job{
		ID: "job-1",
		Options: OCROptions{
			PDFPassword: "transient-secret",
			ProcessMode: "pdf",
		},
	}

	store.clearPDFPassword("job-1")

	job, ok := store.getJob("job-1")
	if !ok {
		t.Fatal("job was unexpectedly removed")
	}
	if job.Options.PDFPassword != "" {
		t.Fatal("PDF password was retained in the job store")
	}
	if job.Options.ProcessMode != "pdf" {
		t.Fatal("clearing the password changed unrelated job options")
	}
}
