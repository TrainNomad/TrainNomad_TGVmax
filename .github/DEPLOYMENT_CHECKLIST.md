# 🚀 Pre-Deployment Checklist - TGVmax v2.0

## Local Verification ✅

Run these commands BEFORE pushing to GitHub:

```bash
cd Backend/tgvmax

# 1. Check all files exist
ls -la main.go api.go types.go converter.go cmd/main.go cmd/converter.go go.mod

# 2. Build binary
go build
ls -lh tgvmax  # Should be ~9.8 MB

# 3. Test converter CLI
go run ./cmd -h
# Should show: -output, -url flags

# 4. Test Go version
go version
# Should be 1.23 or higher

# 5. Verify modules
go mod verify

# 6. Run tests (if any)
go test -v ./...
```

## GitHub Actions Verification ✅

Before merging, ensure:

- [ ] All commits are pushed to GitHub
- [ ] Files exist in repo:
  - `Backend/tgvmax/main.go`
  - `Backend/tgvmax/api.go`
  - `Backend/tgvmax/types.go`
  - `Backend/tgvmax/converter.go`
  - `Backend/tgvmax/cmd/main.go`
  - `Backend/tgvmax/cmd/converter.go`
  - `Backend/tgvmax/go.mod`

- [ ] Workflow file is correct: `.github/workflows/release.yml`
- [ ] Secrets are configured in GitHub repo:
  - `RENDER_TGVMAX_URL` (optional)
  - `RELOAD_SECRET` (optional)

## Deployment Steps

### 1. Push Changes to GitHub

```bash
cd Backend

# Stage files
git add tgvmax/

# Verify changes
git status

# Commit
git commit -m "refactor: harmonize tgvmax API with Europe module (v2.0)

- Extract HTTP handlers to api.go (gzip encoding, harmonized endpoints)
- Consolidate cmd/main.go with data converter (fixes GitHub Actions)
- Update .github/workflows/release.yml with validation and checksums
- Add API.md documentation (endpoints, error handling, examples)
- Reduce main.go to 30 lines (init only)

Features:
- Endpoints: /, /health, /stations, /search (harmonized with Europe)
- Response format: Consistent with Europe module
- gzip support: Accept-Encoding header support
- Validation: File size, magic header, SHA256 checksums
- Zero-downtime: Hot reload without restart

Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com>"

# Push
git push origin main
```

### 2. Verify GitHub Actions Workflow

1. Go to GitHub repo
2. Navigate to **Actions** tab
3. Look for **TGVmax Data Release** workflow
4. Click **"Run workflow"**
5. Monitor the build:
   - ✓ Build step (compiles binary)
   - ✓ Validate step (checks file size)
   - ✓ Release step (creates GitHub release)
   - ✓ Reload step (triggers Render)
   - ✓ Verify step (checks integrity)

### 3. Check Release Output

1. Go to **Releases** page
2. Look for **latest-tgvmax** tag
3. Verify:
   - ✓ `data.bin.gz` file is present
   - ✓ File size is reasonable (10-200 MB)
   - ✓ SHA256 checksum is shown in release notes

### 4. Verify Render Deployment

If `RENDER_TGVMAX_URL` is configured:

```bash
# Check Render status
curl https://your-api.onrender.com/health | jq .

# Should show:
# {
#   "status": "ok",
#   "trains_count": 5000+,
#   "stations_count": 300+,
#   "load_ms": 200-500
# }
```

## Troubleshooting

### Error: "tgvmax/cmd: directory not found"

**Cause**: Files not pushed to GitHub

**Fix**:
```bash
# Verify locally
cd Backend/tgvmax
ls -la cmd/main.go cmd/converter.go

# Ensure they're tracked in git
git status
git add cmd/

# Commit and push
git commit -m "fix: add cmd/ files to repo"
git push origin main
```

### Error: "go run ./cmd: package not found"

**Cause**: Working directory is wrong

**Fix**: Ensure workflow runs from `tgvmax/` directory:
```yaml
- run: |
    mkdir -p tgvmax/data
    cd tgvmax  # Must cd here!
    go run ./cmd -output data/data.bin.gz
```

### Error: "HTTP 401" on reload

**Cause**: `RELOAD_SECRET` not configured or wrong

**Fix**:
1. Go to GitHub repo Settings → Secrets
2. Add `RELOAD_SECRET` secret
3. Verify Render env var matches:
   - `RELOAD_SECRET=<same_value>`

## File Structure Verification

```
Backend/tgvmax/
├── main.go              ✓ 30 lines (init only)
├── api.go               ✓ 350+ lines (HTTP handlers)
├── types.go             ✓ Data structures
├── converter.go         ✓ Binary serialization
├── cmd/
│   ├── main.go          ✓ CLI converter (full implementation)
│   └── converter.go     ✓ Empty/deprecated marker
├── go.mod               ✓ Module definition
├── API.md               ✓ Documentation
└── .github/workflows/
    └── release.yml      ✓ GitHub Actions
```

## Post-Deployment Testing

After successful GitHub Actions run:

```bash
# 1. Download and verify binary
curl -L https://github.com/YOUR_REPO/releases/download/latest-tgvmax/data.bin.gz \
  -o data.bin.gz
file data.bin.gz

# 2. Test locally
./tgvmax &
sleep 2
curl http://localhost:8000/health | jq .

# 3. Verify Render
curl https://your-api.onrender.com/health | jq .

# 4. Test search
curl "https://your-api.onrender.com/search?from=Paris&to=Lyon&limit=5" | jq .
```

## Success Criteria

- [ ] GitHub Actions workflow completes successfully
- [ ] Release is created with `data.bin.gz` asset
- [ ] SHA256 checksum is in release notes
- [ ] Render reload succeeds (if configured)
- [ ] `/health` endpoint returns data
- [ ] `/search` works with real data

---

**Generated**: 2026-09-29  
**Version**: v2.0 (Harmonized with Europe module)
