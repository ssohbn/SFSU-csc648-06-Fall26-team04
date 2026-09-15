# School Scheduler

Team 4's CSC 648 project.

## Run locally

Go 1.26.8 is required:

```sh
cd app
go run .
```

Open <http://localhost:6767>. The health check is available at
<http://localhost:6767/up>.

## Run with Docker

```sh
docker build --tag team04 .
docker run --rm --publish 6767:6767 team04
```

## Deploy with Kamal

The committed Kamal config deploys over SSH to `student@159.198.74.249` and
uses Kamal's local registry. It uses your normal local SSH key, so GitHub
secrets and GHCR credentials are not required.

The server's existing Caddy service remains on ports 80 and 443. Caddy sends
requests to Kamal's proxy on `127.0.0.1:6767`, and that proxy sends them to the
application container on port 6767. The application is not exposed directly.

Before the first deploy, Docker must be installed on the server and `student`
must be allowed to use it. Then install Kamal locally and run setup:

```sh
RBENV_VERSION=3.2.2 rbenv exec gem install kamal --version 2.12.0 --no-document
RBENV_VERSION=3.2.2 rbenv exec kamal setup
```

For later releases:

```sh
RBENV_VERSION=3.2.2 rbenv exec kamal deploy
```

The app is served over HTTP at <http://159.198.74.249>.

For the first cutover, stop the existing `go run main.go` process only after
Docker is ready, then run `kamal setup`. This frees port 6767 for Kamal's proxy;
later `kamal deploy` runs replace the application container without changing
Caddy.
