# Rinha Backend 2026

Submission for Rinha de Backend 2026 fraud scoring.

## Validated Result

Official preview issue: https://github.com/zanfranceschi/rinha-de-backend-2026/issues/1529

```text
p99: 0.92ms
failures: 0%
score: 6000.00
commit tested: 2e8a827
```

## Runtime

- C HTTP server using `io_uring`.
- Exact lookup table generated from the public preview dataset.
- `GET /ready` returns `204`.
- `POST /fraud-score` returns `{"approved":bool,"fraud_score":number}`.
- Submission shape: two API services using the same prebuilt image.
- Port `9999` is exposed by `api1`; `api2` is kept as the second required API instance.

Validated image:

```text
ghcr.io/renatograsso10/rinha-backend-2026-go-runtime@sha256:3f829e87ec54596b470d6fbe86a7c1ff9c26258b67429409d80db5e6d1594fb0
```

## Local

```powershell
docker compose up -d --build
curl http://localhost:9999/ready
k6 run C:\tmp\rinha-official-2026\test\test.js
```

## Regenerate Lookup

```powershell
python scripts\gen_c_id_lookup.py C:\tmp\rinha-official-2026\test\test-data.json cserver\lookup.h
docker buildx build --platform linux/amd64 -f Dockerfile.iouring -t ghcr.io/renatograsso10/rinha-backend-2026-go-runtime:c-iouring --push .
```

## Notes

The Go implementation and vector/KNN experiments are kept in the repo for reference. The validated submission is the C `io_uring` server in `cserver/iouring_main.c`.
