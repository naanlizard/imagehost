#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

export E2E_SUBNET=10.213.77.0/29 E2E_IP=10.213.77.2
BASE=http://$E2E_IP
AUTH=(-u e2e:e2e-password)
TMP=tmp

fail() { echo "FAIL: $*" >&2; exit 1; }
check() {
  [ "$2" = "$3" ] || fail "$1: expected '$2', got '$3'"
  echo "ok: $1"
}
status() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
header() {
  local name=$1
  shift
  curl -s -o /dev/null -D - "$@" | tr -d '\r' | awk -F': ' -v n="$name" 'tolower($1)==tolower(n){print $2}'
}
upload() { header Location "${AUTH[@]}" "$@" "$BASE/upload" | sed 's#^/edit/##'; }
files_on() { curl -s "$BASE/a/$1" | grep -o '/i/[0-9a-f]\{32\}\.[a-z0-9]*' | sort -u; }

rm -rf "$TMP"
mkdir -p "$TMP"
printf 'e2e:%s\n' "$(openssl passwd -apr1 e2e-password)" > "$TMP/htpasswd"
trap 'docker compose down -v >/dev/null 2>&1' EXIT
docker compose down -v >/dev/null 2>&1 || true
docker compose up -d --build --wait
curl -s -o /dev/null --retry 30 --retry-connrefused --retry-delay 1 "${AUTH[@]}" "$BASE/"

cp fixtures/* "$TMP/"
grep -q 'fixture artist' "$TMP/plain.jpg" || fail "jpeg fixture carries no EXIF tag"

APP=$(docker compose ps -q imagehost)
check "app runs as uid 1000" "1000:1000" "$(docker inspect -f '{{.Config.User}}' "$APP")"
check "root filesystem is read-only" true "$(docker inspect -f '{{.HostConfig.ReadonlyRootfs}}' "$APP")"
check "all capabilities dropped" "[ALL]" "$(docker inspect -f '{{.HostConfig.CapDrop}}' "$APP")"
check "no new privileges" "[no-new-privileges:true]" "$(docker inspect -f '{{.HostConfig.SecurityOpt}}' "$APP")"
for c in imagehost imgproxy nginx; do
  check "$c publishes no host ports" "" "$(docker inspect -f '{{range $p, $b := .NetworkSettings.Ports}}{{range $b}}{{.HostPort}} {{end}}{{end}}' "$(docker compose ps -q $c)")"
done

check "home without login" 401 "$(status "$BASE/")"
check "home with login" 200 "$(status "${AUTH[@]}" "$BASE/")"
check "home is not cacheable" "private, no-store, no-transform" "$(header Cache-Control "${AUTH[@]}" "$BASE/")"

TOKEN=$(upload -F 'title=E2E <b>album</b>' -F "files=@$TMP/plain.jpg" -F "files=@$TMP/plain.png")
[[ $TOKEN =~ ^[0-9a-f]{32}$ ]] || fail "upload returned no token: $TOKEN"
JPG=$(files_on "$TOKEN" | grep '\.jpg$')
PNG=$(files_on "$TOKEN" | grep '\.png$')

check "album page" 200 "$(status "$BASE/a/$TOKEN")"
check "album cache header" "no-cache, no-transform" "$(header Cache-Control "$BASE/a/$TOKEN")"
check "album noindex header" "noindex" "$(header X-Robots-Tag "$BASE/a/$TOKEN")"
check "album content type" "text/html" "$(header Content-Type "$BASE/a/$TOKEN")"
check "album nosniff header" "nosniff" "$(header X-Content-Type-Options "$BASE/a/$TOKEN")"
case "$(header Content-Security-Policy "$BASE/a/$TOKEN")" in
  "default-src 'none';"*) echo "ok: album no-script policy header" ;;
  *) fail "album page has no no-script policy header" ;;
esac
curl -s "$BASE/a/$TOKEN" | grep -q 'E2E &lt;b&gt;album&lt;/b&gt;' || fail "title not escaped on album page"
echo "ok: title escaped"

check "image" 200 "$(status "$BASE$JPG")"
check "image cache header" "public, max-age=1209600" "$(header Cache-Control "$BASE$JPG")"
check "image sandbox header" "sandbox" "$(header Content-Security-Policy "$BASE$JPG")"
check "image nosniff header" "nosniff" "$(header X-Content-Type-Options "$BASE$JPG")"
check "image noindex header" "noindex" "$(header X-Robots-Tag "$BASE$JPG")"
check "jpeg content type" "image/jpeg" "$(header Content-Type "$BASE$JPG")"
check "png content type" "image/png" "$(header Content-Type "$BASE$PNG")"

curl -sf -o "$TMP/out.jpg" "$BASE$JPG" || fail "image download failed"
cmp -s "$TMP/plain.jpg" "$TMP/out.jpg" || fail "served file differs from the upload"
echo "ok: file is served as uploaded"

GIF_TOKEN=$(upload -F "files=@$TMP/plain.gif")
check "gif content type" "image/gif" "$(header Content-Type "$BASE$(files_on "$GIF_TOKEN")")"
curl -sf "${AUTH[@]}" "$BASE/edit/$GIF_TOKEN" | grep -q "href=\"$BASE$(files_on "$GIF_TOKEN")\"" || fail "edit page does not link the item's file"
echo "ok: edit page links the item's file"

check "bad token" 404 "$(status "$BASE/a/nothex")"
check "unknown token" 404 "$(status "$BASE/a/00000000000000000000000000000000")"
check "post.json not served" 404 "$(status "$BASE/a/$TOKEN/post.json")"
check "album with trailing slash" 404 "$(status "$BASE/a/$TOKEN/")"
check "file listing" 404 "$(status "$BASE/i/")"
check "404 carries nosniff" "nosniff" "$(header X-Content-Type-Options "$BASE/a/nothex")"
check "query string is ignored" 200 "$(status "$BASE$JPG?v=2")"
check "path traversal needs login" 401 "$(status --path-as-is "$BASE/i/../posts/$TOKEN/post.json")"
check "path traversal with login" 404 "$(status "${AUTH[@]}" -L --path-as-is "$BASE/i/../posts/$TOKEN/post.json")"
check "data folder needs login" 401 "$(status "$BASE/posts/$TOKEN/post.json")"
check "data folder not served by app" 404 "$(status "${AUTH[@]}" "$BASE/posts/$TOKEN/post.json")"

check "page sends no ETag" "" "$(header ETag "$BASE/a/$TOKEN")"
check "page ignores If-Modified-Since" 200 "$(status -H "If-Modified-Since: $(header Last-Modified "$BASE/a/$TOKEN")" "$BASE/a/$TOKEN")"
check "edit" 303 "$(status "${AUTH[@]}" --data-urlencode 'title=Edited' --data-urlencode 'action=down:0' \
  --data-urlencode "file=${JPG#/i/}" --data-urlencode 'desc=first <i>desc</i>' \
  --data-urlencode "file=${PNG#/i/}" --data-urlencode 'desc=' "$BASE/edit/$TOKEN")"
curl -s "$BASE/a/$TOKEN" | grep -q 'first &lt;i&gt;desc&lt;/i&gt;' || fail "description missing or unescaped"
check "order changed" "${PNG#/i/}" "$(curl -s "$BASE/a/$TOKEN" | grep -o 'src="/i/[^"]*' | head -1 | cut -d/ -f3)"

check "cross-site post" 403 "$(status "${AUTH[@]}" -H 'Sec-Fetch-Site: cross-site' -F "files=@$TMP/plain.jpg" "$BASE/upload")"
printf '<svg xmlns="http://www.w3.org/2000/svg"/>' > "$TMP/x.svg"
printf '\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic' > "$TMP/x.heic"
check "heic refused" 415 "$(status "${AUTH[@]}" -F "files=@$TMP/x.heic" "$BASE/upload")"
check "svg refused" 415 "$(status "${AUTH[@]}" -F "files=@$TMP/x.svg" "$BASE/upload")"
{ head -c 4 "$TMP/plain.jpg"; head -c $((101 << 20)) /dev/zero; } > "$TMP/big.jpg"
check "oversize refused" 413 "$(status "${AUTH[@]}" -F "files=@$TMP/big.jpg" "$BASE/upload")"

WIDE_TOKEN=$(upload -F "files=@$TMP/wide.png")
WIDE=$(files_on "$WIDE_TOKEN")
curl -s "$BASE/a/$WIDE_TOKEN" | grep -q ' width="2000" height="1200" ' || fail "album page lacks the image size"
echo "ok: album page carries the image size"
curl -s "$BASE/a/$WIDE_TOKEN" | grep -q "srcset=\"/r/640/avif/${WIDE#/i/} 640w, .*/r/2560/avif/${WIDE#/i/} 2000w\"" || fail "album page lacks the smaller copies"
echo "ok: album page offers smaller copies up to the file's width"
curl -s "${AUTH[@]}" "$BASE/" | grep -q "srcset=\"/r/640/avif/${WIDE#/i/}\"><img loading=\"lazy\" src=\"/r/640/webp/${WIDE#/i/}\"" || fail "home preview is not a thumbnail"
echo "ok: home preview is a thumbnail"
THUMB=/r/640/webp/${WIDE#/i/}
check "thumbnail" 200 "$(status "$BASE$THUMB")"
check "thumbnail content type" "image/webp" "$(header Content-Type "$BASE$THUMB")"
check "thumbnail cache header" "public, max-age=31536000" "$(header Cache-Control "$BASE$THUMB")"
check "thumbnail sandbox header" "sandbox" "$(header Content-Security-Policy "$BASE$THUMB")"
curl -s -o "$TMP/thumb.webp" "$BASE$THUMB"
check "thumbnail container" "VP8X" "$(head -c 16 "$TMP/thumb.webp" | tail -c 4)"
check "thumbnail size" "640 384" "$(od -An -tu1 -j24 -N6 "$TMP/thumb.webp" | awk '{print $1+$2*256+$3*65536+1, $4+$5*256+$6*65536+1}')"
check "avif copy" "image/avif" "$(header Content-Type "$BASE/r/1280/avif/${WIDE#/i/}")"
check "jpeg thumbnail" "image/webp" "$(header Content-Type "$BASE/r/640/webp/${JPG#/i/}")"
check "gif thumbnail" "image/webp" "$(header Content-Type "$BASE/r/640/webp/$(files_on "$GIF_TOKEN" | cut -d/ -f3)")"
check "thumbnail of a missing file" 404 "$(status "$BASE/r/640/webp/00000000000000000000000000000000.png")"
check "width that is not offered" 404 "$(status "$BASE/r/641/webp/${WIDE#/i/}")"
check "format that is not offered" 404 "$(status "$BASE/r/640/png/${WIDE#/i/}")"
check "thumbnail with a format suffix" 404 "$(status "$BASE$THUMB@png")"
check "thumbnail with options" 404 "$(status "$BASE/r/640/webp:rs:fill:9000:9000/${WIDE#/i/}")"
check "imgproxy path needs login" 401 "$(status "$BASE/unsafe/w640:webp/plain/local:///files/${WIDE#/i/}")"
check "imgproxy path not served by app" 404 "$(status "${AUTH[@]}" -L "$BASE/unsafe/w640:webp/plain/local:///files/${WIDE#/i/}")"

MP4=$(files_on "$(upload -F "files=@$TMP/clip.mp4")")
check "no thumbnail for video" 404 "$(status "$BASE/r/640/webp/${MP4#/i/}")"
check "mp4 content type" "video/mp4" "$(header Content-Type "$BASE$MP4")"
check "mp4 range request" 206 "$(status -H 'Range: bytes=0-99' "$BASE$MP4")"
check "webm content type" "video/webm" "$(header Content-Type "$BASE$(files_on "$(upload -F "files=@$TMP/clip.webm")")")"
check "webp content type" "image/webp" "$(header Content-Type "$BASE$(files_on "$(upload -F "files=@$TMP/plain.webp")")")"

docker compose restart imagehost >/dev/null
curl -s -o /dev/null --retry 30 --retry-connrefused --retry-all-errors --retry-delay 1 -f "${AUTH[@]}" "$BASE/"
curl -s "${AUTH[@]}" "$BASE/" | grep -q "/edit/$TOKEN" || fail "post missing from home after restart"
echo "ok: posts reload after restart"

check "remove an item" 303 "$(status "${AUTH[@]}" --data-urlencode 'title=Edited' --data-urlencode 'action=remove:0' \
  --data-urlencode "file=${PNG#/i/}" --data-urlencode 'desc=' \
  --data-urlencode "file=${JPG#/i/}" --data-urlencode 'desc=' "$BASE/edit/$TOKEN")"
check "removed file" 404 "$(status "$BASE$PNG")"
check "kept file" 200 "$(status "$BASE$JPG")"

docker compose stop imgproxy >/dev/null 2>&1
check "thumbnail falls back to the file" "$BASE$WIDE" "$(curl -s -o /dev/null -w '%{redirect_url}' "$BASE$THUMB")"

check "delete" 303 "$(status "${AUTH[@]}" -d confirm=yes "$BASE/delete/$TOKEN")"
check "deleted page" 404 "$(status "$BASE/a/$TOKEN")"
check "deleted file" 404 "$(status "$BASE$JPG")"

echo PASS
