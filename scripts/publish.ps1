param(
    [string]$Image = "ghcr.io/renatograsso10/rinha-backend-2026-go-runtime:latest",
    [string]$Dockerfile = "Dockerfile.iouring"
)

$ErrorActionPreference = "Stop"

if (-not $env:GHCR_USER) {
    throw "Set GHCR_USER"
}
if (-not $env:GHCR_TOKEN) {
    throw "Set GHCR_TOKEN"
}

$env:GHCR_TOKEN | docker login ghcr.io -u $env:GHCR_USER --password-stdin
docker buildx build --platform linux/amd64 -f $Dockerfile -t $Image --push .
