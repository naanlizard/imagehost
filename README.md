# imagehost

A single-user image and video host. The owner uploads behind a login. Anyone with a link can
view. Nothing is listed.

nginx serves pages (`/a/<token>`) and files (`/i/<token>.<ext>`) as static files and puts basic
auth in front of everything else. The app is one Go binary with no third-party modules. It
stores uploads as they are and writes a static page per post. Metadata such as GPS tags is not
removed, so strip it before uploading if it matters. There is no database; a backup is a copy of
the data folder.

## Run

The image is `ghcr.io/naanlizard/imagehost:main`, or build it with `docker build -t imagehost .`.
`test/docker-compose.yml` is a working example of the steps below.

1. Make a data folder owned by uid 1000.
2. Make a login file: `printf 'me:%s\n' "$(openssl passwd -apr1)" > imagehost.htpasswd`. nginx
   does no rate limiting, so use a long generated password.
3. Run the app with the data folder at `/data` and `BASE_URL` set to the public origin, for
   example `https://images.example.com`. `LISTEN_ADDR` is optional and defaults to `:8080`.
4. Run [imgproxy](https://imgproxy.net) with the data folder read-only at `/data` and the
   settings from `test/docker-compose.yml`: local files only, fixed widths and formats. Pages
   load smaller AVIF and WebP copies from it, and a click opens the file as uploaded. If imgproxy
   is down, nginx sends the browser to that file.
5. In nginx, include `deploy/imagehost-locations.conf` in a server block. Mount the data folder
   read-only at `/data` and the login file at `/etc/nginx/imagehost.htpasswd`. The names
   `imagehost` and `imgproxy` must resolve when nginx starts.
6. Put the three containers on one internal Docker network. The app has no login of its own:
   anything that can reach port 8080, including the Docker host, has the owner's rights. Do not
   publish the app's or imgproxy's port with `ports:` or `-p`. Docker opens a published port on
   every interface and bypasses the host firewall.
7. A proxy or CDN in front must allow 100 MiB request bodies and should follow the cache headers
   it is sent. Pages carry `no-transform` so that it does not add anything to them.

One upload is one request of at most 100 MiB, so all its files together must fit under that.

## Caching

Pages are sent with `no-cache`, so an edit shows on the next view. Files are sent with a
two-week cache time and the smaller copies with a one-year cache time, so a deleted file can
stay in a browser or proxy cache that long.

## Tests

`go vet ./... && go test ./...`, then `test/e2e.sh` (needs Docker).
