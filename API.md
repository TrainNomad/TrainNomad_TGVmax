# TGVmax API - Harmonized v2.0

> Status: ✅ Production-ready  
> Architecture: Harmonized with Europe routing engine  
> Last updated: 2026-09-29

## Overview

TGVmax API is a high-performance routing service for SNCF TGVmax trains. It loads train data from a compiled binary format (data.bin.gz) into RAM and provides fast REST endpoints for searching routes and stations.

**Key features:**
- ✅ Sub-10ms search performance
- ✅ ~30-80 MB memory footprint (512 MB limit on Render free tier)
- ✅ Zero-downtime hot reload
- ✅ Harmonized endpoints (matching Europe module)
- ✅ CORS-enabled for frontend consumption

---

## Endpoints

### 1. Root Information

**GET** `/`

Returns API info and available endpoints.

```bash
curl http://localhost:8000/
```

**Response:**
```json
{
  "name": "TGVmax routing API",
  "version": "2.0",
  "endpoints": ["/health", "/stations?q=", "/search?from=&to=&date="]
}
```

---

### 2. Health Check

**GET** `/health`

Returns service status and statistics.

```bash
curl http://localhost:8000/health
```

**Response:**
```json
{
  "status": "ok",
  "timestamp": "2024-01-15T03:00:00Z",
  "trains_count": 5242,
  "stations_count": 312,
  "data_loaded_at": "2024-01-15T02:55:30Z",
  "load_ms": 245,
  "uptime_s": 3600,
  "memory_mb": {
    "heap": 42.5,
    "sys": 85.3
  }
}
```

---

### 3. Station Search

**GET** `/stations?q=<query>&limit=<n>`

Search for stations by name, city, or ID. Returns matching stations.

**Query parameters:**
- `q` (string): Search query (name, city, or ID)
- `limit` (int): Max results [1-50], default 10

```bash
# Search by station name
curl "http://localhost:8000/stations?q=Paris"

# Search by city
curl "http://localhost:8000/stations?q=Lyon&limit=20"

# Get first 5 stations
curl "http://localhost:8000/stations?limit=5"
```

**Response:**
```json
{
  "results": [
    {
      "id": "PGLYON",
      "name": "Paris Gare de Lyon",
      "code": "FRSQL",
      "city": "Paris",
      "country": "France",
      "latitude": 48.8432,
      "longitude": 2.3734
    }
  ],
  "count": 1
}
```

---

### 4. Route Search

**GET** `/search?from=<station>&to=<station>&date=<YYYY-MM-DD>&free_only=<bool>&limit=<n>`

**POST** `/search` (same parameters as query string)

Search for trains between two stations.

**Query parameters:**
- `from` (string, required): Departure station (name, code, or ID)
- `to` (string, required): Arrival station (name, code, or ID)
- `date` (string, optional): Operating day (YYYY-MM-DD). Omit for all dates
- `free_only` (bool, optional): Filter to free seats only (default: false)
- `limit` (int, optional): Max results [1-200], default 50

```bash
# Simple search
curl "http://localhost:8000/search?from=Paris&to=Lyon"

# With date filter
curl "http://localhost:8000/search?from=Paris&to=Lyon&date=2024-01-15"

# Free seats only
curl "http://localhost:8000/search?from=Paris&to=Lyon&free_only=true&limit=10"

# POST variant
curl -X POST http://localhost:8000/search \
  -H "Content-Type: application/json" \
  -d '{"from":"Paris","to":"Lyon","date":"2024-01-15"}'
```

**Response:**
```json
{
  "query": {
    "from": "Paris",
    "to": "Lyon",
    "date": "2024-01-15"
  },
  "results": [
    {
      "id": "TGV_001",
      "train_number": "6521",
      "departure": "Paris Gare de Lyon",
      "arrival": "Lyon Part-Dieu",
      "departure_time": "07:15",
      "arrival_time": "09:45",
      "origin": {
        "id": "PGLYON",
        "name": "Paris Gare de Lyon",
        "city": "Paris",
        "country": "France",
        "latitude": 48.8432,
        "longitude": 2.3734
      },
      "destination": {
        "id": "LPARDIEU",
        "name": "Lyon Part-Dieu",
        "city": "Lyon",
        "country": "France",
        "latitude": 45.7655,
        "longitude": 4.8352
      },
      "available_seats": 145,
      "total_seats": 240,
      "free_seats_only": false,
      "duration_minutes": 150
    }
  ],
  "count": 12
}
```

---

### 5. Data Reload (Admin)

**POST** `/api/tgvmax/reload`

Reload train data from disk without restarting the service.

**Required headers:**
- `X-Reload-Secret`: Reload secret (must match `RELOAD_SECRET` env var)

```bash
curl -X POST http://localhost:8000/api/tgvmax/reload \
  -H "X-Reload-Secret: my-secret-xyz"
```

**Response:**
```json
{
  "status": "success",
  "message": "Data reloaded",
  "duration_ms": 245,
  "timestamp": "2024-01-15T03:15:42Z"
}
```

---

## Error Responses

All errors follow a consistent format:

```json
{
  "error": "error_code",
  "detail": "Human-readable description"
}
```

**Common HTTP status codes:**
- `200 OK` - Successful request
- `400 Bad Request` - Missing or invalid parameters
- `401 Unauthorized` - Missing/invalid reload secret
- `404 Not Found` - Path not found
- `405 Method Not Allowed` - Wrong HTTP method
- `503 Service Unavailable` - Data not loaded yet

---

## Request/Response Formats

### CORS

The API supports CORS with these headers:
```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, POST, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization
```

### Content Encoding

Responses support gzip compression. Include `Accept-Encoding: gzip` in request headers:

```bash
curl -H "Accept-Encoding: gzip" http://localhost:8000/search?from=Paris&to=Lyon
```

### Response Format

All responses are JSON with UTF-8 charset:
```
Content-Type: application/json; charset=utf-8
```

---

## Data Format

### Binary File Format

Train data is compiled into `data.bin.gz`:

```
[Magic: "TGVDATA001" (10 bytes)]
[Version: uint32 (4 bytes)]
[Timestamp: uint64 (8 bytes)]
[Trains section]
  ├─ Count: uint32
  └─ [Train records...]
[Stations section]
  ├─ Count: uint32
  └─ [Station records...]
[gzip compressed]
```

### Key Types

#### StationRef
```json
{
  "id": "PGLYON",
  "name": "Paris Gare de Lyon",
  "code": "FRSQL",
  "city": "Paris",
  "country": "France",
  "latitude": 48.8432,
  "longitude": 2.3734
}
```

#### TrainData (internal)
```json
{
  "id": "TGV_001",
  "train_number": "6521",
  "departure": "Paris Gare de Lyon",
  "arrival": "Lyon Part-Dieu",
  "departure_time": "07:15",
  "arrival_time": "09:45",
  "origin": StationRef,
  "destination": StationRef,
  "available_seats": 145,
  "total_seats": 240,
  "operating_day": "2024-01-15",
  "stops": [StopInfo...]
}
```

#### SearchResult
```json
{
  "id": "TGV_001",
  "train_number": "6521",
  "departure": "Paris Gare de Lyon",
  "arrival": "Lyon Part-Dieu",
  "departure_time": "07:15",
  "arrival_time": "09:45",
  "origin": StationRef,
  "destination": StationRef,
  "available_seats": 145,
  "total_seats": 240,
  "free_seats_only": false,
  "duration_minutes": 150
}
```

---

## Environment Variables

- `PORT` (int): HTTP listen port [default: 8000]
- `RELOAD_SECRET` (string): Secret for `/api/tgvmax/reload` endpoint
- `GITHUB_TOKEN` (string): Optional GitHub token for API rate limits

---

## Performance

Typical performance on Render free tier (512 MB):

| Operation | Time | Notes |
|-----------|------|-------|
| Server startup | 200-500 ms | Data loading + indexing |
| Station search | <5 ms | String search |
| Route search | <10 ms | Filtered scan of trains |
| Health check | <1 ms | Simple stats return |
| Data reload | ~250 ms | Zero downtime |
| Memory usage | 30-80 MB | Depends on data size |

---

## Integration with Europe Module

TGVmax API follows the same patterns as the Europe GTFS routing engine:

| Aspect | Europe | TGVmax |
|--------|--------|--------|
| Data source | GTFS (France, Spain, etc.) | SNCF JSON (TGVmax only) |
| Binary format | network.bin.gz | data.bin.gz |
| Endpoints | `/search`, `/stations` | `/search`, `/stations` |
| Response format | Journey + Leg structure | Simple train results |
| Algorithm | RAPTOR + lower bounds | Indexed filtering |
| Deployment | Docker/Fly.io | Go runtime/Render |

---

## Examples

### Python Client

```python
import requests

BASE_URL = "http://localhost:8000"

# Search for trains
response = requests.get(
    f"{BASE_URL}/search",
    params={
        "from": "Paris",
        "to": "Lyon",
        "date": "2024-01-15",
        "limit": 10
    }
)

for train in response.json()["results"]:
    print(f"{train['train_number']}: {train['departure_time']} -> {train['arrival_time']}")
```

### JavaScript Client

```javascript
const API_BASE = 'http://localhost:8000';

async function searchTrains(from, to, date) {
  const params = new URLSearchParams({ from, to, date, limit: 10 });
  const response = await fetch(`${API_BASE}/search?${params}`);
  return response.json();
}

// Usage
searchTrains('Paris', 'Lyon', '2024-01-15')
  .then(data => console.log(data.results));
```

### cURL Examples

```bash
# Get stations
curl "http://localhost:8000/stations?q=Paris"

# Search trains
curl "http://localhost:8000/search?from=Paris&to=Lyon&date=2024-01-15"

# Search with multiple parameters
curl -G http://localhost:8000/search \
  --data-urlencode "from=Paris" \
  --data-urlencode "to=Lyon" \
  --data-urlencode "date=2024-01-15" \
  --data-urlencode "free_only=true" \
  --data-urlencode "limit=20"

# Check health
curl http://localhost:8000/health | jq .

# Trigger reload
curl -X POST http://localhost:8000/api/tgvmax/reload \
  -H "X-Reload-Secret: my-secret"
```

---

## Deployment

### Local

```bash
# Generate data
go run ./cmd -output data.bin.gz

# Run server
go run main.go api.go types.go converter.go

# Test
curl http://localhost:8000/health
```

### Docker

```bash
docker build -t tgvmax-api .
docker run -p 8000:8000 \
  -e RELOAD_SECRET=secret \
  tgvmax-api
```

### Render

See [DEPLOYMENT.md](./DEPLOYMENT.md) for full setup instructions.

---

## License

- **Code**: TrainNomad Backend (Apache 2.0)
- **Data**: SNCF Open Data ([License](https://ressources.data.sncf.com))

---

**Version**: 2.0 (Harmonized with Europe routing engine)  
**Last updated**: 2026-09-29
