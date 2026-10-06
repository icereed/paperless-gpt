package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Workflows live as plain files next to the global prompts, one directory per
// workflow:
//
//	prompts/workflows/invoices/
//	  workflow.json          name, tags, generation flags, OCR settings
//	  title_prompt.tmpl      only the prompts that differ from the global ones
//
// The directory name is the workflow's ID. Keeping them under prompts/ means
// the volume every installation already mounts for its prompts also keeps the
// workflows, and the prompts are ordinary template files that can be edited,
// copied and versioned like the global ones. Files edited by hand are picked
// up without a restart.
const (
	workflowFileName      = "workflow.json"
	workflowFormatVersion = 1
)

// workflowPromptFiles maps the prompt keys used by the API to file names. They
// match the global prompt files, so a global template can be copied into a
// workflow directory as is.
var workflowPromptFiles = map[string]string{
	"title_prompt":         "title_prompt.tmpl",
	"tag_prompt":           "tag_prompt.tmpl",
	"correspondent_prompt": "correspondent_prompt.tmpl",
	"document_type_prompt": "document_type_prompt.tmpl",
	"date_prompt":          "created_date_prompt.tmpl",
	"custom_field_prompt":  "custom_field_prompt.tmpl",
	"ocr_prompt":           "ocr_prompt.tmpl",
}

// workflowIDPattern keeps IDs usable as a directory name and in URLs, and
// rules out path traversal.
var workflowIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// workflowFile is the on-disk form of a workflow, without ID (the directory
// name) and prompts (separate files).
type workflowFile struct {
	Version                int    `json:"version"`
	Name                   string `json:"name"`
	TriggerTag             string `json:"trigger_tag"`
	CompletionTag          string `json:"completion_tag,omitempty"`
	GenerateTitles         *bool  `json:"generate_titles,omitempty"`
	GenerateTags           *bool  `json:"generate_tags,omitempty"`
	GenerateCorrespondents *bool  `json:"generate_correspondents,omitempty"`
	GenerateCreatedDate    *bool  `json:"generate_created_date,omitempty"`
	GenerateDocumentTypes  *bool  `json:"generate_document_types,omitempty"`
	GenerateCustomFields   *bool  `json:"generate_custom_fields,omitempty"`
	EnableOCR              *bool  `json:"enable_ocr,omitempty"`
	OCRLimitPages          *int   `json:"ocr_limit_pages,omitempty"`
}

// workflowStore holds the workflows read from disk. Every write goes to disk
// first and is then read back, so memory never shows a state the files do not
// have.
type workflowStore struct {
	dir string

	mu        sync.RWMutex
	workflows []WorkflowConfig
	signature string

	// onChange, when set, is called in its own goroutine with the workflows
	// after every reload, e.g. to create their tags in paperless-ngx.
	onChange func([]WorkflowConfig)
}

// OnChange registers a function that sees the workflows after each reload,
// including ones caused by files edited by hand.
func (s *workflowStore) OnChange(fn func([]WorkflowConfig)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

func newWorkflowStore(dir string) *workflowStore {
	return &workflowStore{dir: dir}
}

// workflows is the store the app uses. Tests point it at a temporary directory.
var workflows = newWorkflowStore(filepath.Join("prompts", "workflows"))

// List returns all workflows, sorted by ID.
func (s *workflowStore) List() []WorkflowConfig {
	s.refresh()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneWorkflows(s.workflows)
}

// Get returns the workflow with the given ID.
func (s *workflowStore) Get(id string) (WorkflowConfig, bool) {
	if id == "" {
		return WorkflowConfig{}, false
	}
	for _, wf := range s.List() {
		if wf.ID == id {
			return wf, true
		}
	}
	return WorkflowConfig{}, false
}

// Save writes a workflow. create rejects an existing ID, an update requires
// one. validate sees the workflow and all others and runs under the store's
// lock, so two concurrent saves cannot both pass a uniqueness check.
func (s *workflowStore) Save(wf WorkflowConfig, create bool, validate func(wf WorkflowConfig, others []WorkflowConfig) (int, error)) (int, error) {
	s.refresh()
	s.mu.Lock()
	defer s.mu.Unlock()

	dir, err := s.pathFor(wf.ID)
	if err != nil {
		return http.StatusBadRequest, err
	}
	exists := slices.ContainsFunc(s.workflows, func(other WorkflowConfig) bool { return other.ID == wf.ID })
	if create && exists {
		return http.StatusConflict, fmt.Errorf("workflow with id %q already exists", wf.ID)
	}
	if !create && !exists {
		return http.StatusNotFound, fmt.Errorf("workflow %q not found", wf.ID)
	}
	others := slices.DeleteFunc(cloneWorkflows(s.workflows), func(other WorkflowConfig) bool { return other.ID == wf.ID })
	if validate != nil {
		if status, err := validate(wf, others); err != nil {
			return status, err
		}
	}

	err = writeWorkflowDir(dir, wf)
	s.reloadLocked()
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("saving workflow %q: %w", wf.ID, err)
	}
	return http.StatusOK, nil
}

// Delete removes a workflow and its prompt files.
func (s *workflowStore) Delete(id string) (int, error) {
	s.refresh()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.ContainsFunc(s.workflows, func(wf WorkflowConfig) bool { return wf.ID == id }) {
		return http.StatusNotFound, fmt.Errorf("workflow %q not found", id)
	}
	dir, err := s.pathFor(id)
	if err != nil {
		return http.StatusBadRequest, err
	}
	err = os.RemoveAll(dir)
	s.reloadLocked()
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("deleting workflow %q: %w", id, err)
	}
	return http.StatusOK, nil
}

// pathFor returns the directory of a workflow. IDs come from API requests,
// so this is the one place that turns them into paths: an ID must match
// workflowIDPattern and the result must stay inside the store.
func (s *workflowStore) pathFor(id string) (string, error) {
	if !workflowIDPattern.MatchString(id) || strings.Contains(id, "..") {
		return "", fmt.Errorf("id %q must be lowercase letters, digits, '-' or '_' (at most 64 characters)", id)
	}
	base := filepath.Clean(s.dir)
	dir := filepath.Join(base, id)
	if !strings.HasPrefix(dir, base+string(filepath.Separator)) {
		return "", fmt.Errorf("id %q does not name a directory inside %s", id, s.dir)
	}
	return dir, nil
}

// refresh re-reads the directory when any file in it changed since the last
// read. This is what picks up workflows edited by hand.
func (s *workflowStore) refresh() {
	sig := s.diskSignature()
	s.mu.RLock()
	current := s.signature
	s.mu.RUnlock()
	if sig == current && sig != "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadLocked()
}

// diskSignature summarizes names, sizes and modification times of every file
// in the store. Workflows are a handful of small files, so this is cheap
// enough to run on every poll.
func (s *workflowStore) diskSignature() string {
	var b strings.Builder
	b.WriteString("v1") // never empty, so an empty store is still "read"
	_ = filepath.WalkDir(s.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		fmt.Fprintf(&b, "|%s:%d:%d", path, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return b.String()
}

// reloadLocked reads every workflow directory. Problems with one workflow are
// logged once per change and do not affect the others.
func (s *workflowStore) reloadLocked() {
	s.signature = s.diskSignature()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Errorf("Cannot read workflows from %s: %v", s.dir, err)
		}
		s.workflows = nil
		return
	}
	var loaded []WorkflowConfig
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		wf, err := readWorkflowDir(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			log.Errorf("Workflow %q is not loaded: %v", entry.Name(), err)
			continue
		}
		loaded = append(loaded, wf)
	}
	sort.Slice(loaded, func(i, j int) bool { return loaded[i].ID < loaded[j].ID })
	// Files edited by hand get the same tag rules as the API, so a looping
	// setup cannot sneak in. When two workflows collide, the one with the
	// lower ID wins and the other is reported.
	var valid []WorkflowConfig
	for _, wf := range loaded {
		if _, err := validateWorkflowTags(wf, valid); err != nil {
			log.Errorf("Workflow %q is not loaded: %v", wf.ID, err)
			continue
		}
		valid = append(valid, wf)
	}
	loaded = valid
	s.workflows = loaded
	if s.onChange != nil {
		go s.onChange(cloneWorkflows(loaded))
	}
}

func readWorkflowDir(dir string) (WorkflowConfig, error) {
	id := filepath.Base(dir)
	if !workflowIDPattern.MatchString(id) {
		return WorkflowConfig{}, fmt.Errorf("the directory name must be lowercase letters, digits, '-' or '_'")
	}
	data, err := os.ReadFile(filepath.Join(dir, workflowFileName))
	if err != nil {
		return WorkflowConfig{}, err
	}
	var f workflowFile
	if err := json.Unmarshal(data, &f); err != nil {
		return WorkflowConfig{}, fmt.Errorf("%s: %w", workflowFileName, err)
	}
	if f.Version > workflowFormatVersion {
		return WorkflowConfig{}, fmt.Errorf("%s has version %d, this paperless-gpt reads up to version %d", workflowFileName, f.Version, workflowFormatVersion)
	}
	wf := WorkflowConfig{
		ID:                     id,
		Name:                   f.Name,
		TriggerTag:             strings.TrimSpace(f.TriggerTag),
		CompletionTag:          strings.TrimSpace(f.CompletionTag),
		GenerateTitles:         f.GenerateTitles,
		GenerateTags:           f.GenerateTags,
		GenerateCorrespondents: f.GenerateCorrespondents,
		GenerateCreatedDate:    f.GenerateCreatedDate,
		GenerateDocumentTypes:  f.GenerateDocumentTypes,
		GenerateCustomFields:   f.GenerateCustomFields,
		EnableOCR:              f.EnableOCR,
		OCRLimitPages:          f.OCRLimitPages,
	}
	if wf.TriggerTag == "" {
		return WorkflowConfig{}, fmt.Errorf("%s has no trigger_tag", workflowFileName)
	}
	for key, file := range workflowPromptFiles {
		content, err := os.ReadFile(filepath.Join(dir, file))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return WorkflowConfig{}, err
		}
		if strings.TrimSpace(string(content)) == "" {
			continue
		}
		if wf.Prompts == nil {
			wf.Prompts = map[string]string{}
		}
		wf.Prompts[key] = string(content)
	}
	return wf, nil
}

func writeWorkflowDir(dir string, wf WorkflowConfig) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f := workflowFile{
		Version:                workflowFormatVersion,
		Name:                   wf.Name,
		TriggerTag:             wf.TriggerTag,
		CompletionTag:          wf.CompletionTag,
		GenerateTitles:         wf.GenerateTitles,
		GenerateTags:           wf.GenerateTags,
		GenerateCorrespondents: wf.GenerateCorrespondents,
		GenerateCreatedDate:    wf.GenerateCreatedDate,
		GenerateDocumentTypes:  wf.GenerateDocumentTypes,
		GenerateCustomFields:   wf.GenerateCustomFields,
		EnableOCR:              wf.EnableOCR,
		OCRLimitPages:          wf.OCRLimitPages,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, workflowFileName), buf.Bytes()); err != nil {
		return err
	}
	for key, file := range workflowPromptFiles {
		path := filepath.Join(dir, file)
		content := wf.Prompts[key]
		if strings.TrimSpace(content) == "" {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if err := writeFileAtomic(path, []byte(content)); err != nil {
			return err
		}
	}
	return nil
}

// writeFileAtomic replaces a file in one step, so a reader (or a crash)
// never sees half a workflow file.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func cloneWorkflows(in []WorkflowConfig) []WorkflowConfig {
	out := make([]WorkflowConfig, len(in))
	for i, wf := range in {
		out[i] = wf
		if wf.Prompts != nil {
			out[i].Prompts = make(map[string]string, len(wf.Prompts))
			for k, v := range wf.Prompts {
				out[i].Prompts[k] = v
			}
		}
	}
	return out
}

// migrateWorkflowsFromSettings moves workflows that an earlier build kept in
// config/settings.json into the workflow directory. A workflow whose
// directory already exists is left alone. Each migrated workflow is removed
// from settings.json only after its files were written, so a failure is
// retried on the next start without losing anything.
func migrateWorkflowsFromSettings(store *workflowStore) {
	settingsMutex.Lock()
	defer settingsMutex.Unlock()
	if len(settings.Workflows) == 0 {
		return
	}
	var remaining []WorkflowConfig
	for _, wf := range settings.Workflows {
		if wf.ID == "" || !workflowIDPattern.MatchString(wf.ID) {
			wf.ID = generateWorkflowID(wf.TriggerTag, append(store.List(), remaining...))
		}
		dir, err := store.pathFor(wf.ID)
		if err != nil {
			log.Errorf("Workflow %q from config/settings.json was not migrated: %v", wf.ID, err)
			remaining = append(remaining, wf)
			continue
		}
		if _, err := os.Stat(dir); err == nil {
			// An earlier start may have written the files but failed to
			// update settings.json.
			if onDisk, err := readWorkflowDir(dir); err == nil && strings.EqualFold(onDisk.TriggerTag, wf.TriggerTag) {
				continue
			}
			log.Warnf("Workflow %q from config/settings.json was not migrated: %s already exists. Remove one of them.", wf.ID, dir)
			remaining = append(remaining, wf)
			continue
		}
		if err := writeWorkflowDir(dir, wf); err != nil {
			log.Errorf("Could not migrate workflow %q from config/settings.json to %s: %v. It stays inactive until this works.", wf.ID, dir, err)
			remaining = append(remaining, wf)
			continue
		}
		log.Infof("Migrated workflow %q from config/settings.json to %s", wf.ID, dir)
	}
	settings.Workflows = remaining
	if err := saveSettingsLocked(); err != nil {
		log.Errorf("Workflows were migrated to %s, but config/settings.json could not be updated: %v", store.dir, err)
	}
}
