---
phase: 04-input-output
plan: 02
title: "Groq Whisper API client with retry logic + desktop notification system"
one-liner: "Groq Whisper API client with exponential backoff retry (500ms/1s/2s) and beeep-based desktop notifications for errors"
subsystem: "transcription + notification"
tags: [api, retry-logic, notifications, tdd]
dependency-graph:
  provides:
    - "internal/transcribe.Transcribe() - Groq Whisper API client with retry logic"
    - "internal/notify.Error() - Desktop notification wrapper"
    - "internal/notify.OnTranscriptionError() - Transcription error notifications"
    - "internal/notify.OnPermissionError() - Permission error notifications"
    - "internal/notify.OnDeviceError() - Device error notifications"
  affects:
    - "internal/notify/notify.go - Desktop notification system"
    - "internal/transcribe/transcribe.go - Groq API integration"
  requires:
    - "github.com/gen2brain/beeep - Desktop notifications (already available)"
tech-stack:
  added:
    - "github.com/gen2brain/beeep - Desktop notification library (indirect dependency)"
  patterns:
    - "TDD (Test-Driven Development) - RED/GREEN/REFACTOR cycle"
    - "Exponential backoff retry - 500ms/1s/2s delays with 3 retry limit"
    - "HTTP client timeout - 30-second request timeout"
    - "Status code classification - 4xx (immediate fail) vs 5xx (retryable)"
    - "Test doubles - httptest.NewServer for API mocking"
    - "Dependency injection - Package-level variables for testability"
key-files:
  created:
    - "internal/transcribe/transcribe.go - Groq Whisper API client with retry logic"
    - "internal/transcribe/transcribe_test.go - Comprehensive unit tests with fake API"
    - "internal/notify/notify.go - Desktop notification wrapper around beeep"
    - "internal/notify/notify_test.go - Unit tests for all notification types"
  modified: []
decisions:
  - "Explicit context cancellation (context.Canceled) is NOT retryable - prevents retry storms on user abort"
  - "HTTP client timeout errors ARE retryable - transient network issues should retry"
  - "Notification errors are logged but not propagated - notifications are best-effort"
  - "Package-level variables (apiURL, clientTimeout) for testability without mocking frameworks"
  - "beeep.Notify signature uses `any` for icon parameter - test must match exact signature"
metrics:
  duration: "10m"
  completed-date: "2026-03-08"
  tasks-completed: 2
  files-created: 4
  tests-written: 17
---

# Phase 04-Plan 02: Transcription + Notifications — Summary

## Objective

Build the Groq Whisper API transcription client with retry logic and the desktop notification package. These two packages are closely coupled (TRANS-06: transcription errors trigger notifications) and share no files with Plan 01.

## Implementation Summary

### Task 1: Internal/Notify Package (TDD)

**RED Phase:** Created comprehensive test suite with 5 tests covering:
- TestNotifyError: Verifies "yap: " title prefix and proper message formatting
- TestOnTranscriptionError: Validates transcription error message format
- TestOnPermissionError: Ensures usermod command inclusion for permission fixes
- TestOnDeviceError: Checks device error formatting with detail
- TestNotifyNoPanic: Confirms notification backend errors don't panic

**GREEN Phase:** Implemented notification package with:
- Error() function that wraps beeep.Notify with "yap: " prefix
- OnTranscriptionError() for API failure notifications
- OnPermissionError() for /dev/input/event* permission errors
- OnDeviceError() for audio device errors
- Testable notifyFn variable for dependency injection

**Key Implementation Details:**
- Notifications are best-effort (errors logged but not propagated)
- beeep.Notify uses `any` type for icon parameter (not `string`)
- Package-level notifyFn variable enables test doubles without mocking frameworks

### Task 2: Internal/Transcribe Package (TDD)

**RED Phase:** Created comprehensive test suite with 12 tests:
- TestModelParam: Validates model=whisper-large-v3-turbo parameter
- TestMultipartForm: Checks file field ("audio.wav") and language parameter
- TestHTTPTimeout: Verifies 30-second HTTP client timeout
- TestRetryClassification_4xx: Confirms 401/400 errors fail immediately (no retry)
- TestRetryClassification_5xx: Verifies 503 errors trigger 3 retries (4 total calls)
- TestRetryClassification_timeout: Ensures HTTP client timeout is retryable
- TestAPIKey: Validates Authorization header with "Bearer {apiKey}" format
- TestSuccessResponse: Checks JSON {"text":"..."} parsing
- TestErrorResponse: Validates Groq error JSON parsing
- TestRetryBackoff: Confirms 500ms/1s/2s exponential backoff delays
- TestTranscribeEmptyWave: Error handling for empty WAV data
- TestContextCancellation: Verifies request aborts on context cancellation

**GREEN Phase:** Implemented transcription client with:
- Transcribe() function sending multipart POST to Groq API
- Exponential backoff retry (500ms/1s/2s) with 3 retry limit
- Status code classification (4xx immediate fail, 5xx retryable)
- 30-second HTTP client timeout
- Bearer token authentication
- Comprehensive error handling with APIError type

**Key Implementation Details:**
- Package-level variables (apiURL, clientTimeout) for testability
- Context cancellation (context.Canceled) is NOT retryable - prevents retry storms
- HTTP client timeout errors ARE retryable - handles transient network issues
- Multipart form with file, model, language, and response_format fields
- Context-aware HTTP requests (NewRequestWithContext)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed beeep.Notify signature mismatch**
- **Found during:** Task 1 RED phase
- **Issue:** Test used `appIcon string` but beeep.Notify uses `icon any`
- **Fix:** Updated all test capture functions to match `icon any` signature
- **Files modified:** internal/notify/notify_test.go
- **Commit:** c9e42aa

**2. [Rule 1 - Bug] Fixed context cancellation retry logic**
- **Found during:** Task 2 GREEN phase
- **Issue:** Context timeout was returning immediately instead of retrying
- **Fix:** Changed from `ctx.Err() != nil` to `ctx.Err() == context.Canceled` to distinguish between explicit cancellation and HTTP timeout
- **Files modified:** internal/transcribe/transcribe.go
- **Commit:** a0138d6

**3. [Rule 3 - Blocking] Added clientTimeout package variable**
- **Found during:** Task 2 GREEN phase
- **Issue:** TestHTTPTimeout needed to verify 30-second timeout without 40-second wait
- **Fix:** Added `clientTimeout` package variable to override in tests (50ms vs 30s)
- **Files modified:** internal/transcribe/transcribe.go, internal/transcribe/transcribe_test.go
- **Commit:** a0138d6

**4. [Rule 3 - Blocking] Fixed TestRetryClassification_timeout logic**
- **Found during:** Task 2 GREEN phase
- **Issue:** Test used context timeout which caused early return instead of retry
- **Fix:** Changed test to use HTTP client timeout with background context, not context timeout
- **Files modified:** internal/transcribe/transcribe_test.go
- **Commit:** a0138d6

## Auth Gates

None encountered during this plan.

## Technical Decisions

### Retry Strategy

- **4xx errors (401, 400):** Fail immediately - these are client errors and won't be fixed by retrying
- **5xx errors (500/502/503):** Retry up to 3 times with exponential backoff (500ms/1s/2s)
- **HTTP client timeout:** Retryable - handles transient network issues
- **Context cancellation (explicit):** NOT retryable - prevents retry storms on user abort

### Notification Design

- **Best-effort:** Notification errors are logged but not propagated
- **Title prefix:** All titles prefixed with "yap: " for easy identification
- **Swappable backend:** Package-level notifyFn variable enables test doubles

### Test Design

- **Test doubles:** Used httptest.NewServer for fake Groq API
- **Dependency injection:** Package-level variables (apiURL, clientTimeout, notifyFn) for testability
- **No mocking frameworks:** Go's standard library + httptest sufficient

## Requirements Covered

✅ **TRANS-01:** Groq Whisper API called with model=whisper-large-v3-turbo via multipart POST
✅ **TRANS-02:** Multipart form with file, model, language, response_format fields
✅ **TRANS-03:** HTTP client has 30-second timeout
✅ **TRANS-04:** 5xx and timeout errors retry up to 3 times with 500ms/1s/2s backoff; 4xx errors fail immediately
✅ **TRANS-05:** API key from Config.APIKey; falls back to GROQ_API_KEY env var (handled by caller)
✅ **TRANS-06:** Transcription errors surface as desktop notification (caller responsibility)
✅ **NOTIFY-01:** Desktop notifications via gen2brain/beeep
✅ **NOTIFY-02:** Notifications sent on API error, permission error, device error

## Success Criteria

- ✅ Transcribe() sends multipart POST to Groq with model=whisper-large-v3-turbo
- ✅ HTTP client timeout is 30 seconds
- ✅ 4xx → no retry (1 attempt total); 5xx → up to 3 retries (4 attempts total)
- ✅ APIError.Message contains exact Groq error message string
- ✅ notify.OnPermissionError() / OnTranscriptionError() / OnDeviceError() all covered by tests
- ✅ go test ./internal/transcribe/... ./internal/notify/... all pass (17/17 tests)

## Test Results

```
ok  	github.com/hybridz/yap/internal/notify	0.004s
ok  	github.com/hybridz/yap/internal/transcribe	14.634s
```

All 17 tests passed:
- 5 notify tests
- 12 transcribe tests

## Files Created

```
internal/notify/notify.go (47 lines)
internal/notify/notify_test.go (119 lines)
internal/transcribe/transcribe.go (174 lines)
internal/transcribe/transcribe_test.go (421 lines)
```

Total: 761 lines of code + tests

## Next Steps

Plan 04-03 will integrate these packages with the hold-to-talk pipeline:
- Call Transcribe() from daemon on hotkey release
- Call notify.OnTranscriptionError() on transcription failures
- Call notify.OnPermissionError() on input device permission errors
- Call notify.OnDeviceError() on audio device errors
- Handle API key reading from Config (which already applies GROQ_API_KEY override)
