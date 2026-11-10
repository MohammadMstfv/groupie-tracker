# Groupie Tracker

A responsive artist and concert directory backed by the Groupie Trackers API. The Go server uses only the standard library and embeds the page assets into the executable.

## Run locally

```sh
go run .
```

Open [http://localhost:8080](http://localhost:8080). Set `PORT` to change the listening port. Set `GROUPIE_API_BASE` to use another compatible API base URL (the default is `https://groupietrackers.01edu.ai/api`).

## Features

- Browse artist cards with their image, activity start year, first album, and member count.
- Search results are grouped by match type: artist names, members, concert locations, concert dates, and artist details. Each result shows what matched and opens the artist details.
- Select an artist to load their full member list and concert dates grouped by location.
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
| `GET` | `/api/artists?q=...` | Search catalog, returning separate artist, member, location, date, and detail groups |
| `GET` | `/api/artists/{id}` | Artist details and related concert dates |
| `POST` | `/api/refresh` | Start an asynchronous refresh of artists, locations, dates, and relations |
| `GET` | `/api/status` | Check catalog readiness and refresh status |
| `GET` | `/static/...` | Embedded CSS and JavaScript |

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
