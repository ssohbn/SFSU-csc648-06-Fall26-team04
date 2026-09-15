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
