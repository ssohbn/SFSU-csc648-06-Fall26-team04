# School Scheduler

Team 4's CSC 648 project.

## Run locally

Go 1.26.4 is required:

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

The committed Kamal config deploys the app to `schoolscheduler.me` as the
`ubuntu` user, publishes the image to GitHub Container Registry, and enables
automatic HTTPS. Before the first deploy:

1. Install Docker on the Ubuntu VM and grant the `ubuntu` user access to it.
2. Point `schoolscheduler.me` at the VM and allow inbound ports 80, 443, and 22.
3. Add one repository Actions secret named `KAMAL_SSH_PRIVATE_KEY`. Its public
   key must be authorized for `ubuntu` on the VM.
4. In GitHub Actions, run **Container and deploy**, choose `setup` once, then
   use `deploy` for later releases.

GitHub's short-lived `GITHUB_TOKEN` authenticates to the container registry;
no registry password or application secrets are stored in the repository.
