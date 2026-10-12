package main

import "strings"

// WorkflowRouter decides which tags auto-processing polls and which workflow
// processes a document found under one of them. It is the single place that
// maps documents to workflows, so a different mapping can be plugged in
// without touching the processing pipeline.
type WorkflowRouter interface {
	// PollTags returns the tags to poll, in the order they are processed.
	PollTags() []string
	// WorkflowFor returns the workflow for a document found under pollTag, or
	// nil to process it with the global settings.
	WorkflowFor(doc Document, pollTag string) *WorkflowConfig
}

// triggerTagRouter is the default mapping: every workflow has exactly one
// trigger tag, and a document found under it is processed by that workflow.
// AUTO_TAG maps to the global settings.
type triggerTagRouter struct {
	store *workflowStore
}

// PollTags lists the workflow trigger tags before AUTO_TAG. A document that
// carries both a workflow trigger and AUTO_TAG is then processed once, by the
// workflow, which also takes AUTO_TAG off.
func (r triggerTagRouter) PollTags() []string {
	var tags []string
	for _, wf := range r.store.List() {
		if wf.TriggerTag == "" || strings.EqualFold(wf.TriggerTag, autoTag) {
			continue
		}
		tags = append(tags, wf.TriggerTag)
	}
	return append(tags, autoTag)
}

func (r triggerTagRouter) WorkflowFor(_ Document, pollTag string) *WorkflowConfig {
	if strings.EqualFold(pollTag, autoTag) {
		return nil
	}
	for _, wf := range r.store.List() {
		if strings.EqualFold(wf.TriggerTag, pollTag) {
			return &wf
		}
	}
	return nil
}

// workflowRouter returns the router the app uses.
func (app *App) workflowRouter() WorkflowRouter {
	if app.WorkflowRouter != nil {
		return app.WorkflowRouter
	}
	return triggerTagRouter{store: workflows}
}
