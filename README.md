# ww

A minimal, directory-scoped HTTP file server.

Serves the current directory over HTTP on a random free port, prints the URL,
and shuts down after a period of inactivity.

Mostly made by robots.

## Install

```sh
go install ww@latest
```

Or build locally:

```sh
go build -o ww .
```

## Usage

```sh
ww
```

Serves the current directory. If a directory contains `index.html`, it is
served instead of a listing.

### Flags

| Flag          | Description                                                                  |
| ------------- | ---------------------------------------------------------------------------- |
| `-port`       | Port to listen on (default: random free port in 5001-5999)                   |
| `-listen`     | IP/host to listen on (default: `localhost`)                                  |
| `-url-host`   | Host name shown in the printed URL (default: `localhost`)                    |
| `-dir`        | Directory to serve (default: current directory)                              |
| `-timeout`    | Idle shutdown after no retrievals, e.g. `10m`, `1h 5m`, `30s` (default: 10m) |
| `-no-timeout` | Never shut down on idle                                                      |
