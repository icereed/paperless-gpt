package main

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/tmc/langchaingo/llms"
	"golang.org/x/time/rate"
)

// RateLimitedLLM wraps an LLM client with rate limiting and retry capabilities
type RateLimitedLLM struct {
	llm          llms.Model
	rateLimiter  *rate.Limiter
	maxRetries   int
	backoffMin   time.Duration
	backoffMax   time.Duration
	backoffScale float64
}

// Call implements the llms.Model interface
func (r *RateLimitedLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	if r.rateLimiter != nil {
		if err := r.rateLimiter.Wait(ctx); err != nil {
			return "", fmt.Errorf("rate limiter wait failed: %w", err)
		}
	}

	var lastErr error
	attempt := 0

	for {
		response, err := r.llm.Call(ctx, prompt, options...)
		if err == nil {
			return response, nil
		}

		// Check if we should retry
		if attempt >= r.maxRetries {
			if lastErr != nil {
				return "", fmt.Errorf("all retry attempts failed, last error: %w", lastErr)
			}
			return "", err
		}

		// Calculate exponential backoff with jitter
		backoff := r.backoffMin * time.Duration(1<<uint(attempt))
		if backoff > r.backoffMax {
			backoff = r.backoffMax
		}
		// Add jitter by randomly adjusting +/- 20%
		jitter := time.Duration(float64(backoff) * (0.8 + 0.4*rand.Float64()))

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(jitter):
			// Continue with retry
			attempt++
			lastErr = err
		}
	}
}

// RateLimitConfig holds configuration for rate limiting and retries
type RateLimitConfig struct {
	// RequestsPerMinute is the maximum number of requests allowed per minute
	// If 0 or negative, no rate limiting is applied
	RequestsPerMinute float64

	// MaxRetries is the maximum number of retry attempts
	// If 0 or negative, no retries are attempted
	MaxRetries int

	// BackoffMaxWait is the maximum wait time between retries
	// Defaults to 30 seconds if not specified
	BackoffMaxWait time.Duration
}

// NewRateLimitedLLM creates a new rate-limited LLM client
func NewRateLimitedLLM(llm llms.Model, config RateLimitConfig) *RateLimitedLLM {
	// Set up rate limiter if requests per minute is specified
	var limiter *rate.Limiter
	if config.RequestsPerMinute > 0 {
		// Convert requests per minute to requests per second
		rps := rate.Limit(config.RequestsPerMinute / 60.0)
		limiter = rate.NewLimiter(rps, 1) // Burst size of 1
	}

	// Set default retry values
	maxRetries := config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3 // Default to 3 retries
	}

	backoffMin := 1 * time.Second
	backoffMax := config.BackoffMaxWait
	if backoffMax <= 0 {
		backoffMax = 30 * time.Second // Default to 30 seconds
	}

	return &RateLimitedLLM{
		llm:          llm,
		rateLimiter:  limiter,
		maxRetries:   maxRetries,
		backoffMin:   backoffMin,
		backoffMax:   backoffMax,
		backoffScale: 2.0, // Exponential backoff multiplier
	}
}

// GenerateContent implements the LLM interface with rate limiting and retries
func (r *RateLimitedLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	if r.rateLimiter != nil {
		// Wait for rate limiter
		if err := r.rateLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter wait failed: %w", err)
		}
	}

	var lastErr error
	attempt := 0

	for {
		resp, err := r.llm.GenerateContent(ctx, messages, options...)
		if err == nil {
			// Return the pointer response directly
			return resp, nil
		}

		// Check if we should retry
		if attempt >= r.maxRetries {
			if lastErr != nil {
				return nil, fmt.Errorf("all retry attempts failed, last error: %w", lastErr)
			}
			return nil, err
		}

		// Calculate exponential backoff with jitter
		backoff := r.backoffMin * time.Duration(1<<uint(attempt))
		if backoff > r.backoffMax {
			backoff = r.backoffMax
		}
		// Add jitter by randomly adjusting +/- 20%
		jitter := time.Duration(float64(backoff) * (0.8 + 0.4*rand.Float64()))

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(jitter):
			// Continue with retry
			attempt++
			lastErr = err
		}
	}
}
