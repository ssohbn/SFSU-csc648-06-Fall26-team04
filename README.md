# group project
StudySync: find, join and run study groups.

# setup & install

## deps
sqlite3 and golang (1.26+) are required for this project

for debian based distros just hit this and ur good!
``sudo apt install sqlite3 golang gcc``

make sure to export ``CGO_ENABLED=1`` because we use ``https://github.com/mattn/go-sqlite3``

## running the project
in your terminal, ``cd`` to ``app/`` and run ``go run .``

open http://localhost:6767. on first run the app creates ``studysync.sqlite3``
and applies everything in ``app/migrations/`` in order, so a fresh clone works
without a shared database file.

| env var | default | |
|---|---|---|
| ``PORT`` | ``6767`` | port to listen on |
| ``DB_PATH`` | ``studysync.sqlite3`` | sqlite file to use |
| ``GEMINI_API_KEY`` | unset | server-only Gemini key; AI matching is disabled without it |
| ``GEMINI_MODEL`` | ``gemini-3.5-flash-lite`` | Gemini model for study group matching |

schema changes go in a new numbered file in ``app/migrations/`` (never edit one
that has shipped). ``GET /up`` returns ``ok`` for health checks.

## docker
from the repo root:

```
docker build -t studysync .
docker run -p 6767:6767 -v studysync_data:/data studysync
```

the database lives in the ``studysync_data`` volume, so it survives new
containers. put ``-seed`` at the end of the run command for demo data.

## deploy
[kamal](https://kamal-deploy.org) builds the image on your machine, runs it on
the server behind kamal-proxy and swaps containers without downtime. the setup
is in ``config/deploy.yml``.

```
gem install kamal
export KAMAL_HOST=<server ip>
export KAMAL_SSH_USER=root   # optional, the default is student
kamal setup     # first time, also starts kamal-proxy
kamal deploy    # after that
kamal app logs  # the first boot prints the demo account password
```

- ``kamal setup`` installs docker on the server if the ssh user is root or has
  passwordless sudo. otherwise someone with sudo has to install docker there
  and run ``sudo usermod -aG docker student``.
- kamal-proxy takes ports 80 and 443 and gets a let's encrypt cert for each
  host in ``proxy.host``. the dns records have to point straight at the server
  (dns only in cloudflare, not proxied) or the cert check fails. stop anything
  else on those ports before ``kamal setup``.
- kamal deploys the last commit, not uncommitted changes.
- the image is built for amd64, change ``builder.arch`` if the server is arm64.

## tests
``cd app && go test ./...``

## AI study group matching

Sign in and open **AI matches** (`/matches`). The form combines your saved
bio/interests with optional courses, availability and study preferences.
Selecting **Find my matches** sends those details, joined groups' meeting times
and eligible group descriptions to Google Gemini. Form details are not saved.
Email, username, invite codes, coordinates and other members' profiles are
excluded. Only groups you can join are candidates; joined groups are excluded.

Set the key in the server environment without putting it in shell history:

```sh
read -r -s GEMINI_API_KEY
export GEMINI_API_KEY
cd app
go run . -seed
```

The server uses structured JSON, low thinking and no search grounding. It
validates returned group IDs, scores and reasons, drops explicit schedule
conflicts, and rechecks group visibility before displaying results. Free-text
meeting times cannot prove a conflict-free calendar; unknown times are marked
for confirmation. The prompt and schema live in `app/matching.go`.

One server process allows one call at a time, at most 15 calls per rolling
minute and 500 per rolling 24 hours. Requests are capped at 12KB including the
prompt and schema, reserving 16,000 input tokens per call (at most 240,000 TPM,
below the configured 250,000). Output is capped at 2,048 tokens. At most 30
candidates fit one request; courses and shared interests take priority, and
the page shows how many were compared. These limits reset with the process;
Google enforces project-wide quotas shared with other users and processes.
Calls time out after 30 seconds and are never automatically retried. Missing
keys, provider errors and quota limits show an explicit message and leave
regular group search available.

For Docker, pass the exported key at runtime with `--env GEMINI_API_KEY`
and optionally `--env GEMINI_MODEL`; do not add it to the image. For Kamal,
copy `.kamal/secrets.example` to `.kamal/secrets` and keep its environment
reference unchanged. Export the key in the deploy shell. `config/deploy.yml`
then passes it as a Kamal secret; deployments without an exported key leave
AI matching disabled. Never replace the reference with a literal key.

Normal tests use a test-only provider and make no Gemini calls. To verify the
real signed-in HTTP → SQLite → Gemini → results path with synthetic student
and group data, export a working key and run from `app/`:

```sh
STUDYSYNC_GEMINI_LIVE=1 go test -count=1 -run TestGeminiLiveMatching -v
```

This opt-in test uses one real request. It fails on authentication, quota,
invalid output, missing expected matches or an explicit scheduling conflict.
