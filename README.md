# Rinha Backend 2026

Backend submission for the Rinha de Backend 2026 fraud scoring challenge.

The runtime is a Go HTTP server that classifies requests with k-nearest-neighbor search over the official reference dataset. It does not use preview payload IDs, expected preview answers, or generated lookup tables.

## API

```text
GET  /ready
POST /fraud-score
```

`GET /ready` returns `204 No Content`.

`POST /fraud-score` returns:

```json
{"approved":true,"fraud_score":0}
```

or:

```json
{"approved":false,"fraud_score":1}
```

## Runtime Layout

The official submission uses two containers with the same API image:

- `api1`: exposes host port `9999`, CPU `0.90`, memory `250MB`.
- `api2`: second required API instance, CPU `0.10`, memory `100MB`.

Total resource limit: `1 CPU / 350MB`.

The submission image tag:

```text
ghcr.io/renatograsso10/rinha-backend-2026-go-runtime:knn
```

## Repository Structure

```text
cmd/
internal/
  Go implementation and vector search

proxy/
  lightweight C proxy experiment

scripts/
  publish.ps1           pushes linux/amd64 image to GHCR
  check_compliance.sh   checks for forbidden lookup artifacts

docker-compose.yml      local/runtime compose shape
Dockerfile              runtime image build
```

## Run Locally

Requirements:

- Docker Desktop
- `k6`
- official challenge repo cloned locally, with test data at `C:\tmp\rinha-official-2026`

Start the backend:

```powershell
docker compose up -d --build
```

Smoke test:

```powershell
curl.exe http://localhost:9999/ready

curl.exe -X POST http://localhost:9999/fraud-score `
  -H "Content-Type: application/json" `
  -d "{\"id\":\"tx-1329056812\"}"
```

Run the official preview test locally:

```powershell
k6 run C:\tmp\rinha-official-2026\test\test.js
```

Rebuild and push the runtime image:

```powershell
docker buildx build `
  --platform linux/amd64 `
  -f Dockerfile `
  -t ghcr.io/renatograsso10/rinha-backend-2026-go-runtime:knn `
  --push .
```

## Submission

The `submission` branch contains the official submission files:

- `docker-compose.yml`
- `info.json`

It references prebuilt GHCR images only, as required by the challenge validator.

## Compliance

Run this before publishing:

```sh
sh scripts/check_compliance.sh
```
