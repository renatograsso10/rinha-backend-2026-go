# Rinha Backend 2026 Go

Go + fasthttp + compact kd-tree index for Rinha de Backend 2026 fraud scoring.

## Local

```powershell
Copy-Item C:\tmp\rinha-official-2026\resources -Destination . -Recurse
docker compose up -d --build
curl http://localhost:9999/ready
```

## Publish

```powershell
$env:GHCR_USER='renatograsso10'
$env:GHCR_TOKEN='<classic PAT with write:packages>'
.\scripts\publish.ps1
```
