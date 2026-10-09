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

func TestProcessJobClearsPDFPasswordOnTerminalError(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	if err := env.db.AutoMigrate(&OCRRun{}); err != nil {
		t.Fatal(err)
	}

	previousStore := jobStore
	defer func() { jobStore = previousStore }()
	jobStore = &JobStore{jobs: make(map[string]*Job)}
	job := &Job{
		ID:         "job-terminal-error",
		DocumentID: 42,
		Options: OCROptions{
			PDFPassword:     "transient-secret",
			ReplaceOriginal: true,
			UploadPDF:       false,
		},
	}
	jobStore.addJob(job)

	// This invalid option combination returns before any external document
	// work, while still exercising processJob's terminal error path.
	processJob(&App{Database: env.db}, job)

	stored, ok := jobStore.getJob(job.ID)
	if !ok {
		t.Fatal("job was unexpectedly removed")
	}
	if stored.Options.PDFPassword != "" {
		t.Fatal("processJob retained the transient PDF password")
	}
}
