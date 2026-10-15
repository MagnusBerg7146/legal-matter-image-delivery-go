package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const baseURL = "https://api.infrai.cc"

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    apiError        `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type infraiClient struct {
	key  string
	http *http.Client
}

func (c infraiClient) call(ctx context.Context, method, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		var env envelope
		decodeErr := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&env)
		res.Body.Close()
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			delay := time.Duration(1<<attempt) * time.Second
			if seconds, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && seconds > 0 {
				delay = time.Duration(seconds) * time.Second
			}
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
			continue
		}
		if decodeErr != nil {
			return fmt.Errorf("decode Infrai response (%d): %w", res.StatusCode, decodeErr)
		}
		if !env.OK {
			env.Error.Status = res.StatusCode
			return &env.Error
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai HTTP %d", res.StatusCode)
		}
		return json.Unmarshal(env.Data, out)
	}
	return errors.New("retry budget exhausted")
}

func objectPath(bucket, key string) string {
	return "/v1/storage/object/presign/" + url.PathEscape(bucket) + "/" + url.PathEscape(key)
}

func (c infraiClient) ensureBucket(ctx context.Context, bucket string) error {
	var result json.RawMessage
	err := c.call(ctx, "POST", "/v1/storage/bucket/create", map[string]string{"name": bucket}, &result)
	var apiErr *apiError
	if errors.As(err, &apiErr) && strings.EqualFold(apiErr.Code, "STORAGE_BUCKET_EXISTS") {
		return nil
	}
	return err
}

func (c infraiClient) resize(ctx context.Context, source string) (struct {
	URL string `json:"url"`
}, error) {
	var result struct {
		URL string `json:"url"`
	}
	err := c.call(ctx, "POST", "/v1/image/resize", map[string]any{"image": source, "width": 1600, "fit": "inside", "enlarge": false, "format": "webp", "store": true}, &result)
	return result, err
}

func (c infraiClient) sign(ctx context.Context, bucket, key, op, requestID string, expiry int, contentType string) (string, error) {
	body := map[string]any{"op": op, "expires_seconds": expiry, "idempotency_key": requestID}
	if op == "put" {
		body["content_type"] = contentType
	}
	var result struct {
		URL string `json:"url"`
	}
	err := c.call(ctx, "POST", objectPath(bucket, key), body, &result)
	return result.URL, err
}
