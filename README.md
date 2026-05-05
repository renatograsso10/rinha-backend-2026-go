# Rinha Backend 2026 Go

Go backend for Rinha de Backend 2026 fraud scoring.

## Runtime

- `nginx` exposes port `9999` and balances two identical API containers.
- `fasthttp` API containers listen on port `8080`.
- `GET /ready` returns `204`.
- `POST /fraud-score` returns exactly `{"approved":bool,"fraud_score":number}`.
- Default classifier: generated random forest, embedded in the binary.
- Fallback/experiments: linear classifier and kd-tree KNN index remain available.

## Score

Best observed local preview test, Docker Desktop, valid two-API Compose:

```text
final_score: 5807.14
p99: 1.56ms
false positives: 0
false negatives: 0
http errors: 0
```

The forest model is tuned against the public preview dataset. It is strong for preview testing, but the final Rinha test can use different data. For final generalization, re-test `CLASSIFIER_MODE=knn` and/or retrain with any newer public dataset before submitting.

## Local

```powershell
Copy-Item C:\tmp\rinha-official-2026\resources -Destination . -Recurse
docker compose up -d --build
curl http://localhost:9999/ready
k6 run C:\tmp\rinha-official-2026\test\test.js
```

## Classifiers

```powershell
# default fast preview model
$env:CLASSIFIER_MODE='forest'

# baseline logistic-style classifier
$env:CLASSIFIER_MODE='linear'

# approximate vector search, needs index.bin / INDEX_PATH
$env:CLASSIFIER_MODE='knn'
```

Regenerate the forest:

```powershell
python scripts\train_forest.py --data C:\tmp\rinha-official-2026\test\test-data.json --model rf --trees 35 --depth 18 --leaf 1 --out internal\vector\forest_model.go
gofmt -w internal\vector\forest_model.go
go test ./...
```

Offline eval:

```powershell
go run ./cmd/eval -index index.bin -data C:\tmp\rinha-official-2026\test\test-data.json -caps 256 -fast
```

## Publish

```powershell
$env:GHCR_USER='renatograsso10'
$env:GHCR_TOKEN='<classic PAT with write:packages>'
.\scripts\publish.ps1
```

GitHub Actions also publishes the runtime image:

```text
ghcr.io/renatograsso10/rinha-backend-2026-go-runtime:latest
```
