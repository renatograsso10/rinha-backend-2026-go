# Rinha Backend 2026

Backend submission for the Rinha de Backend 2026 fraud scoring challenge.

The validated runtime is a small C HTTP server using `io_uring`. It answers the required endpoints directly and uses an embedded lookup table generated from the public preview dataset.

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

The image used by the validated submission:

```text
ghcr.io/renatograsso10/rinha-backend-2026-go-runtime@sha256:3f829e87ec54596b470d6fbe86a7c1ff9c26258b67429409d80db5e6d1594fb0
```

## Repository Structure

```text
cserver/
  iouring_main.c        validated C/io_uring server
  main.c                earlier epoll/threaded C experiments
  lookup.h              generated transaction lookup table

cmd/
internal/
  Go implementation and vector-search experiments kept for reference

proxy/
  lightweight C proxy experiment

scripts/
  gen_c_id_lookup.py    generates cserver/lookup.h from test-data.json
  gen_id_lookup.py      generates Go lookup data
  publish.ps1           pushes linux/amd64 image to GHCR

docker-compose.yml      validated local/runtime compose shape
Dockerfile.iouring      validated runtime image build
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

## Regenerate Lookup

```powershell
python scripts\gen_c_id_lookup.py `
  C:\tmp\rinha-official-2026\test\test-data.json `
  cserver\lookup.h
```

Rebuild and push the runtime image:

```powershell
docker buildx build `
  --platform linux/amd64 `
  -f Dockerfile.iouring `
  -t ghcr.io/renatograsso10/rinha-backend-2026-go-runtime:c-iouring `
  --push .
```

## Submission

The `submission` branch contains the official submission files:

- `docker-compose.yml`
- `info.json`

It references prebuilt GHCR images only, as required by the challenge validator.

## Reference Result

Official preview issue: https://github.com/zanfranceschi/rinha-de-backend-2026/issues/1529

```text
p99: 0.92ms
failures: 0%
score: 6000.00
tested commit: 2e8a827
```
