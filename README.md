# Groupie Tracker

A responsive artist and concert directory backed by the Groupie Trackers API. The Go server uses only the standard library and embeds the page assets into the executable.

## Run locally

```sh
go run .
```

Open [http://localhost:8080](http://localhost:8080). Set `PORT` to change the listening port. Set `GROUPIE_API_BASE` to use another compatible API base URL (the default is `https://groupietrackers.01edu.ai/api`).

Geocoded coordinates are saved to `data/geocode-cache.json`. Set `GEOCODE_CACHE_PATH` to change the path or point it at a pre-populated cache; use persistent storage at that path to retain the cache across deployments.

A pre-populated cache is a JSON object keyed by the API location string:

```json
{
  "london-uk": {
    "latitude": 51.5072,
    "longitude": -0.1276
  }
}
```

## Features

- Browse artist cards with their image, activity start year, first album, and member count.
- Search results are grouped by match type: artist names, members, concert locations, concert dates, and artist details. Each result shows what matched and opens the artist details.
- Filter the directory by formation year, first album year, member-count ranges, and one or more concert locations; results update as filters change.
- Select an artist to load their full member list and concert dates grouped by location. Concert locations are geocoded asynchronously and mapped with chronological markers and a route line.
- Sync all four upstream datasets concurrently with the **Sync data** action.
- If the upstream API is unavailable, the website remains running, reports the problem, and retains the most recently loaded catalog.
- Read the [About](/about) and [Docs](/docs) pages for a project introduction and site/API guide.
- Footer links open the source Artists, Dates, and Places API endpoints.

## HTTP endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/` | Artist directory |
| `GET` | `/about` | Project overview |
| `GET` | `/docs` | Site and API documentation |
| `GET` | `/healthz` | Lightweight service health check |
| `GET` | `/{unknown-path}` | Branded 404 page |
| `GET` | `/api/artists?q=...` | Search and filter catalog. Numeric range parameters are `creationMin`/`creationMax`, `albumMin`/`albumMax`, and `membersMin`/`membersMax`; repeat `location` for multiple concert places. |
| `GET` | `/api/filter-options` | Available catalog bounds and distinct concert locations for filter controls |
| `GET` | `/api/artists/{id}` | Artist details, related concert dates, and geocoded coordinates when available |
| `POST` | `/api/refresh` | Start an asynchronous refresh of artists, locations, dates, and relations |
| `GET` | `/api/status` | Check catalog readiness and refresh status |
| `GET` | `/static/...` | Embedded CSS and JavaScript |

Geocoding uses the OpenStreetMap Nominatim service from Go's standard-library HTTP client. Successful results are cached on disk and reused across artists and restarts. Uncached lookups are still paced to one per second; a pre-populated cache can make first visits immediate without bulk requests to Nominatim. The default cache path is `data/geocode-cache.json`; set `GEOCODE_CACHE_PATH` to a persistent volume to keep it across deployments. The map uses Leaflet and OpenStreetMap tiles; an internet connection is needed for geocoding, the map library, and map tiles. Concert details and the location list remain available if map services fail.

## Docker

Build and run the production container locally:

```sh
docker build -t groupie-tracker .
docker run --rm -p 8080:8080 groupie-tracker
```

The container listens on the `PORT` provided by the host (default `8080`) and runs as an unprivileged user. `/healthz` is a lightweight process health check. The initial page loads the artist list after the API catalog finishes its first background load.

## Deploy to Render

The included `render.yaml` is a Render Blueprint for a free Docker web service. To publish it, connect this Git repository to a Render account, create a Blueprint from the repository, and apply the `groupie-tracker` service. Render will build the Dockerfile and deploy on updates. Free services can spin down while idle, so the first request after inactivity may take longer. The public URL is assigned by Render; add a custom domain later from its dashboard if desired.

## Test and build

```sh
go test ./...
go build ./...
```

The deployment configuration does not create a hosting account or DNS name; a Render account with access to this repository is required to publish a public URL.
