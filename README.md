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

## tests
``cd app && go test ./...``
