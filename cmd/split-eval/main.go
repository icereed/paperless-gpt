// split-eval uploads concatenated PDFs to a dry-run instance and scores exact boundaries.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

type options struct {
	input, paperlessURL, gptURL, token string
	batchSize                          int
}

// main reads evaluation flags and environment settings, then runs with the
// configured timeout. Nonpositive timeouts and evaluation errors are printed to
// stderr and exit with status 1.
func main() {
	var opts options
	flag.StringVar(&opts.input, "input", "", "Folder of single-document PDFs, read in filename order")
	flag.StringVar(&opts.paperlessURL, "paperless-url", os.Getenv("PAPERLESS_BASE_URL"), "paperless-ngx URL")
	flag.StringVar(&opts.gptURL, "gpt-url", "http://localhost:8080", "paperless-gpt URL with SPLIT_DRY_RUN=true")
	flag.IntVar(&opts.batchSize, "batch-size", 3, "Maximum source documents per batch (at least 2)")
	timeout := flag.Duration("timeout", 20*time.Minute, "Timeout for the entire evaluation")
	flag.Parse()
	opts.token = os.Getenv("PAPERLESS_API_TOKEN")
	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeout must be positive")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := evaluate(ctx, opts, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// validateURL rejects raw unless it is an HTTP(S) URL with a host and without
// embedded credentials, a query or a fragment.
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("URLs must use HTTP(S), with no credentials, query or fragment")
	}
	return nil
}

// evaluate uploads batches of opts.input PDFs in filename order and writes
// boundary scores to out, using ctx for requests and polling. It checks dry-run
// mode before each upload and suggestion request; uploaded documents are retained
// in paperless-ngx. Invalid inputs, PDF processing and API failures return errors.
func evaluate(ctx context.Context, opts options, out io.Writer) error {
	if opts.input == "" || opts.batchSize < 2 || opts.token == "" {
		return fmt.Errorf("provide -input, -batch-size >= 2 and PAPERLESS_API_TOKEN")
	}
	for _, raw := range []string{opts.paperlessURL, opts.gptURL} {
		if err := validateURL(raw); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(opts.input)
	if err != nil {
		return err
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".pdf") {
			files = append(files, filepath.Join(opts.input, entry.Name()))
		}
	}
	if len(files) < 2 {
		return fmt.Errorf("input folder must contain at least two PDFs")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ngx := endpoint{client, strings.TrimRight(opts.paperlessURL, "/"), opts.token}
	gpt := endpoint{client, strings.TrimRight(opts.gptURL, "/"), ""}
	var totalTP, totalFP, totalFN int
	for start := 0; start < len(files); start += opts.batchSize {
		if err := requireDryRun(ctx, gpt); err != nil {
			return err
		}
		batch := files[start:min(start+opts.batchSize, len(files))]
		pdf, truth, pages, err := mergeBatch(batch)
		if err != nil {
			return err
		}
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, err := form.CreateFormFile("document", "split-eval.pdf")
		if err != nil {
			return err
		}
		if _, err := part.Write(pdf); err != nil {
			return err
		}
		title := fmt.Sprintf("Split evaluation %s batch %d", time.Now().UTC().Format("20060102T150405.000000000"), start/opts.batchSize+1)
		if err := form.WriteField("title", title); err != nil {
			return err
		}
		if err := form.Close(); err != nil {
			return err
		}
		var task string
		if err := ngx.request(ctx, "POST", "/api/documents/post_document/", &body, form.FormDataContentType(), &task); err != nil {
			return err
		}
		if task == "" {
			return fmt.Errorf("upload returned no task ID")
		}
		fmt.Fprintf(out, "batch=%d task=%s sources=%d pages=%d\n", start/opts.batchSize+1, task, len(batch), pages)
		id, err := waitForDocument(ctx, ngx, task)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "document_id=%d (retained in paperless-ngx)\n", id)
		if err := requireDryRun(ctx, gpt); err != nil {
			return err
		}
		var suggestion struct {
			Status     string `json:"status"`
			PageCount  int    `json:"page_count"`
			Boundaries []struct {
				AfterPage int `json:"after_page"`
			} `json:"boundaries"`
		}
		path := fmt.Sprintf("/api/documents/%d/split-suggestions", id)
		if err := gpt.request(ctx, "POST", path, nil, "", &suggestion); err != nil {
			return err
		}
		if suggestion.Status != "review" || suggestion.PageCount != pages {
			return fmt.Errorf("document %d: expected a Review Suggestion with %d pages", id, pages)
		}
		predicted := make([]int, 0, len(suggestion.Boundaries))
		previous := 0
		for _, boundary := range suggestion.Boundaries {
			if boundary.AfterPage <= previous || boundary.AfterPage >= pages {
				return fmt.Errorf("document %d: invalid predicted boundaries", id)
			}
			predicted = append(predicted, boundary.AfterPage)
			previous = boundary.AfterPage
		}
		tp, fp, fn := reportBoundaries(out, pages, truth, predicted)
		fmt.Fprintf(out, "precision=%s recall=%s TP=%d FP=%d FN=%d\n", ratio(tp, tp+fp), ratio(tp, tp+fn), tp, fp, fn)
		totalTP, totalFP, totalFN = totalTP+tp, totalFP+fp, totalFN+fn
	}
	fmt.Fprintf(out, "total precision=%s recall=%s TP=%d FP=%d FN=%d\n", ratio(totalTP, totalTP+totalFP), ratio(totalTP, totalTP+totalFN), totalTP, totalFP, totalFN)
	return nil
}

// mergeBatch concatenates files in order and returns PDF bytes, one-based page
// numbers to split after, and the total page count. Reading,
// counting or merging failures, including empty source PDFs, return an error.
func mergeBatch(files []string) ([]byte, []int, int, error) {
	readers := make([]io.ReadSeeker, 0, len(files))
	var truth []int
	total := 0
	for index, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, 0, err
		}
		count, err := api.PageCount(bytes.NewReader(data), nil)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("count pages in %s: %w", path, err)
		}
		if count == 0 {
			return nil, nil, 0, fmt.Errorf("empty PDF: %s", path)
		}
		total += count
		if index < len(files)-1 {
			truth = append(truth, total)
		}
		readers = append(readers, bytes.NewReader(data))
	}
	var pdf bytes.Buffer
	if err := api.MergeRaw(readers, &pdf, false, nil); err != nil {
		return nil, nil, 0, err
	}
	return pdf.Bytes(), truth, total, nil
}

type endpoint struct {
	client      *http.Client
	base, token string
}

// request sends method and body to e.base+path using ctx, adding token and
// content-type headers when set, then decodes response JSON into result with an
// 8 MiB read limit. It returns request, transport, non-2xx status or decode errors;
// HTTP status errors omit the response body.
func (e endpoint) request(ctx context.Context, method, path string, body io.Reader, contentType string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, e.base+path, body)
	if err != nil {
		return err
	}
	if e.token != "" {
		req.Header.Set("Authorization", "Token "+e.token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(result)
}

// requireDryRun reads gpt's split configuration using ctx and returns an error
// if the request fails or dry-run mode is not reported as enabled.
func requireDryRun(ctx context.Context, gpt endpoint) error {
	var list struct {
		Config struct {
			DryRun bool `json:"dry_run"`
		} `json:"config"`
	}
	if err := gpt.request(ctx, "GET", "/api/split-suggestions", nil, "", &list); err != nil {
		return err
	}
	if !list.Config.DryRun {
		return fmt.Errorf("evaluation requires SPLIT_DRY_RUN=true")
	}
	return nil
}

// waitForDocument polls ngx for task with one-second waits until it returns a
// document ID, a request or task error occurs, or ctx is canceled.
func waitForDocument(ctx context.Context, ngx endpoint, task string) (int, error) {
	for {
		var payload json.RawMessage
		if err := ngx.request(ctx, "GET", "/api/tasks/?task_id="+url.QueryEscape(task), nil, "", &payload); err != nil {
			return 0, err
		}
		id, err := taskDocumentID(payload)
		if err != nil || id != 0 {
			return id, err
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// taskDocumentID reads a task array or paginated results from payload and returns
// the first successful task's available document ID, preferring result_data.
// It returns zero when no ID is available, or an error for malformed JSON or an
// encountered failed or revoked task.
func taskDocumentID(payload []byte) (int, error) {
	var tasks []struct {
		Status  string `json:"status"`
		Related []int  `json:"related_document_ids"`
		Result  struct {
			ID int `json:"document_id"`
		} `json:"result_data"`
	}
	if bytes.HasPrefix(bytes.TrimSpace(payload), []byte("{")) {
		var page struct {
			Results json.RawMessage `json:"results"`
		}
		if err := json.Unmarshal(payload, &page); err != nil {
			return 0, err
		}
		payload = page.Results
	}
	if err := json.Unmarshal(payload, &tasks); err != nil {
		return 0, err
	}
	for _, task := range tasks {
		switch strings.ToUpper(task.Status) {
		case "SUCCESS":
			if task.Result.ID > 0 {
				return task.Result.ID, nil
			}
			if len(task.Related) > 0 && task.Related[0] > 0 {
				return task.Related[0], nil
			}
		case "FAILURE", "FAILED", "REVOKED":
			return 0, fmt.Errorf("upload task failed; inspect paperless-ngx task log")
		}
	}
	return 0, nil
}

// reportBoundaries writes expected and predicted splits after each one-based
// page number before pages to out, returning true positives, false positives and
// false negatives from exact matches against truth and predicted.
func reportBoundaries(out io.Writer, pages int, truth, predicted []int) (tp, fp, fn int) {
	fmt.Fprintln(out, "after_page\texpected\tpredicted\tresult")
	for page := 1; page < pages; page++ {
		expected, detected := slices.Contains(truth, page), slices.Contains(predicted, page)
		result := "TN"
		switch {
		case expected && detected:
			result = "TP"
			tp++
		case detected:
			result = "FP"
			fp++
		case expected:
			result = "FN"
			fn++
		}
		fmt.Fprintf(out, "%d\t%t\t%t\t%s\n", page, expected, detected, result)
	}
	return
}

// ratio formats numerator/denominator to four decimal places, or returns "n/a"
// when denominator is zero.
func ratio(numerator, denominator int) string {
	if denominator == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.4f", float64(numerator)/float64(denominator))
}
