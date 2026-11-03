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
