package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type intake struct {
	MatterID string `json:"matter_id"`
	Stage    string `json:"stage"`
	Image    string `json:"image"`
}

func deliveryWindow(stage string) (int, bool) {
	switch stage {
	case "intake":
		return 900, true
	case "signed_document":
		return 3600, true
	case "deadline_followup":
		return 300, true
	default:
		return 0, false
	}
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		panic("set INFRAI_API_KEY")
	}
	bucket := os.Getenv("LEGAL_IMAGE_BUCKET")
	if bucket == "" {
		bucket = "legal-delivery-images"
	}
	client := infraiClient{key: key, http: &http.Client{Timeout: 45 * time.Second}}
	if err := client.ensureBucket(context.Background(), bucket); err != nil {
		panic(err)
	}
	http.HandleFunc("POST /matters/image", func(w http.ResponseWriter, r *http.Request) {
		var input intake
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20)).Decode(&input); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		ttl, valid := deliveryWindow(input.Stage)
		if !valid || input.MatterID == "" || input.Image == "" {
			http.Error(w, "matter_id, image, and valid stage required", 400)
			return
		}
		if _, err := url.ParseRequestURI(input.Image); err != nil {
			http.Error(w, "image must be a URL", 400)
			return
		}
		// The same key and base URL handle image optimization and private object delivery.
		optimized, err := client.resize(r.Context(), input.Image)
		if err != nil {
			respondError(w, err)
			return
		}
		if optimized.URL == "" {
			http.Error(w, "missing optimized image URL", 502)
			return
		}
		digest := sha256.Sum256([]byte(input.MatterID + "\x00" + input.Stage + "\x00" + input.Image))
		objectKey := hex.EncodeToString(digest[:]) + ".webp"
		putURL, err := client.sign(r.Context(), bucket, objectKey, "put", objectKey+":put", 600, "image/webp")
		if err != nil {
			respondError(w, err)
			return
		}
		imageReq, err := http.NewRequestWithContext(r.Context(), "GET", optimized.URL, nil)
		if err != nil {
			http.Error(w, "invalid optimized URL", 502)
			return
		}
		imageRes, err := client.http.Do(imageReq)
		if err != nil {
			http.Error(w, "optimized image fetch failed", 502)
			return
		}
		defer imageRes.Body.Close()
		if imageRes.StatusCode != 200 {
			http.Error(w, "optimized image fetch failed", 502)
			return
		}
		putReq, err := http.NewRequestWithContext(r.Context(), "PUT", putURL, io.LimitReader(imageRes.Body, 12<<20))
		if err != nil {
			http.Error(w, "invalid upload URL", 502)
			return
		}
		putReq.Header.Set("Content-Type", "image/webp")
		putRes, err := client.http.Do(putReq)
		if err != nil {
			http.Error(w, "image upload failed", 502)
			return
		}
		putRes.Body.Close()
		if putRes.StatusCode < 200 || putRes.StatusCode >= 300 {
			http.Error(w, "image upload failed", 502)
			return
		}
		getURL, err := client.sign(r.Context(), bucket, objectKey, "get", objectKey+":get:"+input.Stage, ttl, "")
		if err != nil {
			respondError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"matter_id": input.MatterID, "stage": input.Stage, "object_key": objectKey, "download_url": getURL, "expires_seconds": ttl})
	})
	fmt.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

func respondError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if e, ok := err.(*apiError); ok && e.Status >= 400 && e.Status < 500 {
		status = e.Status
	}
	http.Error(w, strings.TrimSpace(err.Error()), status)
}
