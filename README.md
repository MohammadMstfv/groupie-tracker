# On Tour — Groupie Tracker

A responsive artist and concert directory backed by the Groupie Trackers API. The Go server uses only the standard library and embeds the page assets into the executable.

## Run locally

```sh
go run .
```

Open [http://localhost:8080](http://localhost:8080). Set `PORT` to change the listening port. Set `GROUPIE_API_BASE` to use another compatible API base URL (the default is `https://groupietrackers.01edu.ai/api`).

## Features

- Browse artist cards with their image, activity start year, first album, and member count.
- Search artists, members, and concert locations. Search runs in the browser asynchronously and requests filtered results from the Go server.
- Select an artist to load their full member list and concert dates grouped by location.
- Sync all four upstream datasets concurrently with the **Sync data** action.
- If the upstream API is unavailable, the website remains running, reports the problem, and retains the most recently loaded catalog.

## HTTP endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/` | Artist directory |
| `GET` | `/api/artists?q=...` | Search by artist, member, or location |
| `GET` | `/api/artists/{id}` | Artist details and related concert dates |
| `POST` | `/api/refresh` | Start an asynchronous refresh of artists, locations, dates, and relations |
| `GET` | `/api/status` | Check catalog readiness and refresh status |
| `GET` | `/static/...` | Embedded CSS and JavaScript |

## Test and build

```sh
go test ./...
go build ./...
```

To deploy, build the executable with `go build -o groupie-tracker .` and run it on a Go-compatible host that exposes the configured `PORT`. The application does not include a hosting provider or DNS configuration.
