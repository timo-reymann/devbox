# Repository instructions

## README.md must stay in sync (required)

`README.md` documents this image's ports and every environment variable a
user can set to change behaviour or expose something. Update `README.md`
**in the same change** whenever you:

- add, remove or rename a port, change a bind address, or touch `EXPOSE`
  in the `Dockerfile` or any script under `entrypoints/`;
- add, remove or rename an environment variable, or change its default or
  effect (`entrypoints/*`, `caddy/Caddyfile`, `dashboard/*`, Dockerfile
  `ENV`/`ARG`-exposed runtime behaviour);
- add or remove a service in the `multirun` `ENTRYPOINT`;
- change how CLI tokens (`GITHUB_TOKEN`, `GITLAB_TOKEN`), the ngrok CLI or
  the dashboard links work;
- change a coupling listed under "Keep in sync" in `README.md` (then update
  that list too).

Never leave `README.md` describing behaviour the code no longer has, and do
not merge a change that alters ports/env vars with a stale README.
