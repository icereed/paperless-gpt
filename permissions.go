package main

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

// enqueuePermissionRestore adds a pending permission restore request to the queue.
func (app *App) enqueuePermissionRestore(taskID string, originalDocID int, owner *int, permissions *Permissions) {
	app.pendingRestoresMu.Lock()
	defer app.pendingRestoresMu.Unlock()

	app.pendingRestores = append(app.pendingRestores, PendingPermissionRestore{
		TaskID:        taskID,
		OriginalDocID: originalDocID,
		Owner:         owner,
		Permissions:   permissions,
		CreatedAt:     time.Now(),
	})
}

// processPendingPermissionRestores processes the queue of pending permission restores.
// It is called periodically from the background loop.
func (app *App) processPendingPermissionRestores(ctx context.Context) (int, error) {
	app.pendingRestoresMu.Lock()
	queue := app.pendingRestores
	app.pendingRestores = nil
	app.pendingRestoresMu.Unlock()

	if len(queue) == 0 {
		return 0, nil
	}

	processed := 0
	var remaining []PendingPermissionRestore

	for _, entry := range queue {
		// Expire entries older than 24 hours
		if time.Since(entry.CreatedAt) > 24*time.Hour {
			logrus.WithFields(logrus.Fields{
				"task_id":      entry.TaskID,
				"original_doc": entry.OriginalDocID,
			}).Warn("Permission restore request expired after 24h, dropping")
			continue
		}

		taskStatus, err := app.Client.GetTaskStatus(ctx, entry.TaskID)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"task_id":      entry.TaskID,
				"original_doc": entry.OriginalDocID,
				"error":        err,
			}).Warn("Failed to check task status for permission restore, will retry")
			remaining = append(remaining, entry)
			continue
		}

		status, ok := taskStatus["status"].(string)
		if !ok {
			logrus.WithFields(logrus.Fields{
				"task_id":      entry.TaskID,
				"original_doc": entry.OriginalDocID,
			}).Warn("Could not determine task status for permission restore, will retry")
			remaining = append(remaining, entry)
			continue
		}

		switch status {
		case "SUCCESS":
			logger := logrus.WithFields(logrus.Fields{
				"task_id":      entry.TaskID,
				"original_doc": entry.OriginalDocID,
			})
			app.patchNewDocumentPermissions(ctx, taskStatus, entry.Owner, entry.Permissions, logger)
			logger.Info("Permission restore completed successfully")
			processed++

		case "FAILURE":
			logrus.WithFields(logrus.Fields{
				"task_id":      entry.TaskID,
				"original_doc": entry.OriginalDocID,
			}).Warn("Document processing failed, permission restore not possible")

		default:
			// PENDING or STARTED — retry next cycle
			remaining = append(remaining, entry)
		}
	}

	// Put back remaining entries for next cycle
	if len(remaining) > 0 {
		app.pendingRestoresMu.Lock()
		app.pendingRestores = append(app.pendingRestores, remaining...)
		app.pendingRestoresMu.Unlock()
	}

	return processed, nil
}
