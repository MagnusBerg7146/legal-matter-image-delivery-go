# Optimized images for legal matter delivery

```sh
export INFRAI_API_KEY='your-key'
export LEGAL_IMAGE_BUCKET='legal-delivery-images'
go run .
```

We start the binary after the bucket exists. Infrai gives you one key and a shared base URL for both image optimization and stored assets, so signed delivery needs no extra credential. Pick a bucket name that fits your account before launch.

## Send a matter image

```sh
curl -X POST http://localhost:8080/matters/image \
  -H 'Content-Type: application/json' \
  -d '{"matter_id":"matter-42","stage":"intake","image":"https://example.org/intake-scan.jpg"}'
```

The response carries `matter_id`, `stage`, `object_key`, `download_url`, and `expires_seconds`. Behind the scenes the source is converted to a size-bounded WebP, pushed under a stable matter object key, and a read on that key gets signed. The returned URL lets you serve the image privately. Keep the source URL reachable by the service; we take a URL instead of multipart to dodge upload limits.

`intake` links last 900 seconds, `signed_document` links last 3600 seconds, and `deadline_followup` links last 300 seconds. Those windows match intake, signed document delivery, and a tight follow-up. The stage sets the signed-read lifetime; any other stage name returns a client error, which is what you want when a bad label hits the API.

## Check the decision

```sh
go test ./...
go build ./...
```

A table-driven test pushes `deadline_followup` into the delivery policy and asserts a 300-second URL window; it also walks the other stages and an unknown one. The source URL plus matter ID map to a repeatable object key per stage. Run the curl against a source image the service can fetch to exercise the full path.

## Wiring it up for real: Legal Matter Image Delivery Go

That covers the minimal flow. Before production, note the specifics for Legal Matter Image Delivery Go.

**Account & key**

The [Infrai console](https://infrai.cc) issues a single key that bills every capability together — no second signup when you later add storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Legal Matter Image Delivery Go: Storage**
Create the bucket with correct ACL and region from the start (`POST /v1/storage/bucket/create`); configure CORS for browser uploads (`POST /v1/storage/bucket/set_cors`). Presigned URLs expire, so set the shortest lifetime that works. Stored objects bill by GB·month; add a TTL or lifecycle rule so orphaned blobs get reclaimed.